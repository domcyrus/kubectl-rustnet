package runner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/domcyrus/kubectl-rustnet/internal/pod"
)

// Exercise real subprocess arguments and cancellation, while replacing only
// kubectl. The log makes any deletion before flush/copy verification visible.
func fakeKubectl(t *testing.T, scenario string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_LOG"
while [ "${1#--}" != "$1" ]; do shift 2; done
op=$1
shift
case "$op" in
 run) [ "$FAKE_SCENARIO" != create-failure ] ;;
 get)
  if [ "$FAKE_SCENARIO" = startup-failure ]; then
   printf '%s' '{"status":{"phase":"Failed","containerStatuses":[{"name":"rustnet-debug","state":{"terminated":{"exitCode":42}}}]}}'
   exit 0
  fi
  printf '%s' '{"status":{"phase":"Running","containerStatuses":[{"name":"rustnet-debug","state":{"running":{}}},{"name":"rustnet-export","state":{"running":{}}}]}}' ;;
 attach)
  case "$FAKE_SCENARIO" in
   cancel|timeout) exec sleep 30 ;;
   attach-failure) exit 1 ;;
  esac ;;
 exec)
  while [ "$1" != -- ]; do shift; done
  shift
  case "$1" in
   touch) [ "$FAKE_SCENARIO" != stop-failure ] ;;
   /bin/sh)
    case "$FAKE_SCENARIO" in
     flush-pending) printf running ;;
     capture-failure) printf 42 ;;
     *) printf 0 ;;
    esac ;;
   sha256sum)
    [ "$FAKE_SCENARIO" != missing-file ] || exit 1
    printf '%s  %s\n' "$FAKE_HASH" "$2" ;;
   cat)
    [ "$FAKE_SCENARIO" != copy-failure ] || exit 1
    if [ "$FAKE_SCENARIO" = corrupt ]; then printf corrupt; else printf '%s' "$FAKE_CONTENT"; fi ;;
   *) exit 2 ;;
  esac ;;
 delete) [ "$FAKE_SCENARIO" != delete-failure ] ;;
 *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_SCENARIO", scenario)
	t.Setenv("FAKE_CONTENT", "capture evidence\n")
	t.Setenv("FAKE_HASH", fmt.Sprintf("%x", sha256.Sum256([]byte("capture evidence\n"))))
	return log
}

func exportOptions(t *testing.T) Options {
	t.Helper()
	return Options{Namespace: "test-ns", Kubeconfig: "/tmp/test config", Context: "test-context", OutputDir: t.TempDir(), Pod: pod.Options{Image: "rustnet:test", RustnetArgs: []string{"--headless"}}}
}

func TestExportsFinishBeforeDelete(t *testing.T) {
	for _, format := range []string{"both", "jsonl", "pcapng", "pcap"} {
		t.Run(format, func(t *testing.T) {
			log := fakeKubectl(t, "normal")
			opts := exportOptions(t)
			opts.OutputFormat = format
			if err := run(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			calls := string(data)
			stop := strings.Index(calls, "-- touch /evidence/.stop")
			flush := strings.Index(calls, "-- /bin/sh -c if")
			copy := strings.LastIndex(calls, "-- cat /evidence/")
			deletion := strings.Index(calls, " delete pod ")
			if stop < 0 || stop >= flush || flush >= copy || copy >= deletion {
				t.Fatalf("incorrect cleanup order:\n%s", calls)
			}
			for _, line := range strings.Split(strings.TrimSpace(calls), "\n") {
				if !strings.HasPrefix(line, "--namespace test-ns --kubeconfig /tmp/test config --context test-context ") {
					t.Errorf("lost cluster selection: %s", line)
				}
			}
			dirs, _ := os.ReadDir(opts.OutputDir)
			if len(dirs) != 1 {
				t.Fatalf("session directories: %v", dirs)
			}
			files, _ := pod.ExportFiles(format)
			for _, file := range files {
				path := filepath.Join(opts.OutputDir, dirs[0].Name(), file)
				contents, err := os.ReadFile(path)
				if err != nil || string(contents) != "capture evidence\n" {
					t.Fatalf("%s: %q, %v", path, contents, err)
				}
				info, _ := os.Stat(path)
				if info.Mode().Perm() != 0600 {
					t.Fatalf("evidence permissions: %v", info.Mode())
				}
			}
		})
	}
}

func TestExportFailuresRetainPod(t *testing.T) {
	for _, scenario := range []string{"stop-failure", "missing-file", "copy-failure", "corrupt"} {
		t.Run(scenario, func(t *testing.T) {
			log := fakeKubectl(t, scenario)
			err := run(context.Background(), exportOptions(t))
			if err == nil || !strings.Contains(err.Error(), "retained") || !strings.Contains(err.Error(), "'cp' '-c' 'rustnet-export'") {
				t.Fatalf("missing recovery instructions: %v", err)
			}
			calls, _ := os.ReadFile(log)
			if strings.Contains(string(calls), " delete pod ") {
				t.Fatalf("deleted evidence after failure:\n%s", calls)
			}
		})
	}
}

func TestSessionErrorsStillSaveEvidence(t *testing.T) {
	for _, scenario := range []string{"attach-failure", "capture-failure", "delete-failure"} {
		t.Run(scenario, func(t *testing.T) {
			log := fakeKubectl(t, scenario)
			err := run(context.Background(), exportOptions(t))
			if err == nil {
				t.Fatal("error was swallowed")
			}
			calls, _ := os.ReadFile(log)
			if !strings.Contains(string(calls), "-- cat /evidence/capture.pcapng") || !strings.Contains(string(calls), " delete pod ") {
				t.Fatalf("missing export/cleanup:\n%s", calls)
			}
		})
	}
}

func TestTimeoutAndCancellationUseExportCleanup(t *testing.T) {
	for _, scenario := range []string{"timeout", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			log := fakeKubectl(t, scenario)
			opts := exportOptions(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "timeout" {
				opts.Timeout = 30 * time.Millisecond
			}
			done := make(chan error, 1)
			go func() { done <- run(ctx, opts) }()
			if scenario == "cancel" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					calls, _ := os.ReadFile(log)
					if strings.Contains(string(calls), " attach ") {
						cancel()
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("attach did not start")
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup did not finish")
			}
			calls, _ := os.ReadFile(log)
			if strings.Count(string(calls), " delete pod ") != 1 || strings.Count(string(calls), "-- touch /evidence/.stop") != 1 {
				t.Fatalf("cleanup must run once:\n%s", calls)
			}
		})
	}
}

func TestFlushTimeoutDoesNotCopy(t *testing.T) {
	log := fakeKubectl(t, "flush-pending")
	opts := exportOptions(t)
	s := session{opts: opts, name: "test-pod"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := s.stopCapture(ctx); err == nil {
		t.Fatal("expected flush timeout")
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "-- cat /evidence/capture") || strings.Contains(string(calls), " delete pod ") {
		t.Fatal("evidence touched before flush")
	}
}

func TestInvalidExportsFailBeforeCreatingPod(t *testing.T) {
	for _, change := range []func(*Options){
		func(o *Options) { o.OutputFormat = "invalid" },
		func(o *Options) { o.OutputDir = ""; o.OutputFormat = "jsonl" },
		func(o *Options) { o.Pod.RustnetArgs = []string{"--json-log=/tmp/log"} },
		func(o *Options) { o.Pod.RustnetArgs = []string{"--pcap-export", "/tmp/capture"} },
		func(o *Options) { o.Pod.RustnetArgs = []string{"--pcapng-export=/tmp/capture"} },
	} {
		log := fakeKubectl(t, "normal")
		opts := exportOptions(t)
		change(&opts)
		if err := run(context.Background(), opts); err == nil {
			t.Fatal("expected validation error")
		}
		if data, _ := os.ReadFile(log); len(data) != 0 {
			t.Fatalf("called kubectl for invalid options: %s", data)
		}
	}
}

func TestNonExportFailuresStillCleanUp(t *testing.T) {
	for _, scenario := range []string{"create-failure", "startup-failure"} {
		t.Run(scenario, func(t *testing.T) {
			log := fakeKubectl(t, scenario)
			opts := exportOptions(t)
			opts.OutputDir = ""
			if err := run(context.Background(), opts); err == nil {
				t.Fatal("failure was swallowed")
			}
			calls, _ := os.ReadFile(log)
			if strings.Count(string(calls), " delete pod ") != 1 {
				t.Fatalf("missing cleanup after failure: %s", calls)
			}
		})
	}
}
