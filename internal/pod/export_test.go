package pod

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureWrapperPreservesStdinAndPublishesCompletion(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	for name, script := range map[string]string{
		"rustnet": "#!/bin/sh\nIFS= read -r key || exit 42\nprintf '%s' \"$key\" > \"$TEST_EVIDENCE/connections.jsonl\"\n",
		// File ownership needs root in production; leave it unchanged in this test.
		"chown": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Exercise the actual wrapper without requiring a root-owned /evidence.
	script := strings.ReplaceAll(captureScript, ExportPath, "\"$TEST_EVIDENCE\"")
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script, "rustnet")
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "TEST_EVIDENCE="+dir)
	cmd.Stdin = strings.NewReader("q\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wrapper failed: %v: %s", err, output)
	}
	for file, want := range map[string]string{"connections.jsonl": "q", ".exit-code": "0\n"} {
		got, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", file, got, err, want)
		}
	}
}
