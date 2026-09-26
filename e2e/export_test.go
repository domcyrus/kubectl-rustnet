package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Set RUSTNET_EXPORT_IMAGE to an image with graceful signal shutdown and the
// Kubernetes attribution fix from rustnet#634. These tests use real exports.
func exportImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv("RUSTNET_EXPORT_IMAGE")
	if image == "" {
		t.Skip("set RUSTNET_EXPORT_IMAGE to run capture export tests")
	}
	return image
}

func exportSession(t *testing.T, format string, extra []string, interrupt os.Signal) string {
	t.Helper()
	image := exportImage(t)
	dir := t.TempDir()
	args := []string{"--image", image, "--output-dir", dir, "--output-format", format}
	if interrupt != nil {
		args = append(args, "--timeout", "1m")
	} else if len(extra) == 0 {
		args = append(args, "--timeout", "4s")
	}
	args = append(args, "--", "--headless", "--output", "json", "--no-geoip")
	args = append(args, extra...)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pluginBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if interrupt != nil {
		// Wait until RustNet has installed handlers and opened its output writers.
		waitForExportCapture(t)
		time.Sleep(2 * time.Second)
		if err := cmd.Process.Signal(interrupt); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("plugin: %v\n%s", err, stderr.String())
	}
	dirs, err := os.ReadDir(dir)
	if err != nil || len(dirs) != 1 {
		t.Fatalf("missing session directory: %v, %v", dirs, err)
	}
	session := filepath.Join(dir, dirs[0].Name())
	var podName string
	for _, line := range strings.Split(stderr.String(), "\n") {
		if strings.HasPrefix(line, "Debug pod: ") {
			_, podName, _ = strings.Cut(strings.TrimPrefix(line, "Debug pod: "), "/")
		}
	}
	if podName == "" {
		t.Fatalf("missing pod name: %s", stderr.String())
	}
	if output, err := exec.Command("kubectl", "get", "pod", podName, "--ignore-not-found", "-o", "name").CombinedOutput(); err != nil || len(bytes.TrimSpace(output)) != 0 {
		t.Fatalf("debug pod still present: %s (%v)", output, err)
	}
	t.Logf("saved %s after pod %s was deleted", session, podName)
	return session
}

func waitForExportCapture(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command("kubectl", "get", "pods", "-l", "app=rustnet-debug", "-o", "json").Output()
		if err == nil {
			var list struct {
				Items []struct {
					Metadata struct {
						Name string `json:"name"`
					} `json:"metadata"`
				} `json:"items"`
			}
			if json.Unmarshal(output, &list) == nil {
				for _, p := range list.Items {
					if err := exec.Command("kubectl", "exec", p.Metadata.Name, "-c", "rustnet-export", "--", "test", "-s", "/evidence/capture.pcapng").Run(); err == nil {
						return
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("capture did not start")
}

func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("invalid JSONL: %v", err)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatalf("no connection evidence in %s", path)
	}
	return rows
}

func checkPCAPNG(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 28 || binary.LittleEndian.Uint32(data[:4]) != 0x0a0d0d0a {
		t.Fatal("missing PCAPNG section header")
	}
	packets := 0
	for len(data) > 0 {
		if len(data) < 12 {
			t.Fatal("truncated PCAPNG block")
		}
		length := int(binary.LittleEndian.Uint32(data[4:8]))
		if length < 12 || length%4 != 0 || length > len(data) || binary.LittleEndian.Uint32(data[length-4:length]) != uint32(length) {
			t.Fatal("invalid PCAPNG block length")
		}
		if binary.LittleEndian.Uint32(data[:4]) == 6 {
			packets++
		}
		data = data[length:]
	}
	if packets == 0 {
		t.Fatal("no captured packets")
	}
	t.Logf("verified %d complete PCAPNG packet blocks", packets)
}

func TestExportNormalExit(t *testing.T) {
	dir := exportSession(t, "both", []string{"--duration", "4"}, nil)
	readJSONL(t, filepath.Join(dir, "connections.jsonl"))
	checkPCAPNG(t, filepath.Join(dir, "capture.pcapng"))
}

func TestExportTimeout(t *testing.T) {
	dir := exportSession(t, "pcap", nil, nil)
	readJSONL(t, filepath.Join(dir, "capture.pcap.connections.jsonl"))
	data, err := os.ReadFile(filepath.Join(dir, "capture.pcap"))
	if err != nil || len(data) <= 24 {
		t.Fatalf("missing PCAP packets: %v", err)
	}
	if binary.LittleEndian.Uint32(data[:4]) != 0xa1b2c3d4 {
		t.Fatalf("invalid PCAP magic: %x", data[:4])
	}
}

func TestExportInterrupt(t *testing.T) {
	for name, signal := range map[string]os.Signal{"SIGINT": os.Interrupt, "SIGTERM": syscall.SIGTERM} {
		t.Run(name, func(t *testing.T) {
			dir := exportSession(t, "both", nil, signal)
			readJSONL(t, filepath.Join(dir, "connections.jsonl"))
			checkPCAPNG(t, filepath.Join(dir, "capture.pcapng"))
		})
	}
}

func TestExportShortKubernetesFlows(t *testing.T) {
	exportImage(t)
	workloadImage := os.Getenv("RUSTNET_EXPORT_WORKLOAD_IMAGE")
	if workloadImage == "" {
		t.Skip("set RUSTNET_EXPORT_WORKLOAD_IMAGE to a Python image")
	}
	name := fmt.Sprintf("rustnet-short-flows-%d", time.Now().UnixNano())
	receiver := name + "-receiver"
	serverScript := `import socket
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("0.0.0.0",18080)); s.listen(4096)
u=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); u.bind(("0.0.0.0",19090))
while True: u.recvfrom(1024)
`
	if output, err := exec.Command("kubectl", "run", receiver, "--image", workloadImage, "--image-pull-policy=IfNotPresent", "--restart=Never", "--", "python", "-u", "-c", serverScript).CombinedOutput(); err != nil {
		t.Fatalf("receiver: %v %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("kubectl", "delete", "pod", receiver, "--grace-period=0", "--force").Run() })
	if output, err := exec.Command("kubectl", "wait", "--for=condition=Ready", "pod/"+receiver, "--timeout=60s").CombinedOutput(); err != nil {
		t.Fatalf("receiver readiness: %v %s", err, output)
	}
	ip, err := exec.Command("kubectl", "get", "pod", receiver, "-o", "jsonpath={.status.podIP}").Output()
	if err != nil {
		t.Fatal(err)
	}
	// Each client process finishes both sockets and exits well inside the 5s
	// namespace scan. A separate receiver pod makes traffic visible on veths.
	script := fmt.Sprintf(`import subprocess,sys,time
child='import socket; s=socket.socket(); s.connect(("%s",18080)); s.close(); u=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); u.bind(("0.0.0.0",0)); u.sendto(b"evidence",("%s",19090)); u.close()'
while True:
 subprocess.run([sys.executable,'-c',child])
 time.sleep(0.1)
`, ip, ip)
	command := exec.Command("kubectl", "run", name, "--image", workloadImage, "--image-pull-policy=IfNotPresent", "--restart=Never", "--", "python", "-u", "-c", script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("workload: %v %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("kubectl", "delete", "pod", name, "--grace-period=0", "--force").Run() })
	if output, err := exec.Command("kubectl", "wait", "--for=condition=Ready", "pod/"+name, "--timeout=60s").CombinedOutput(); err != nil {
		t.Fatalf("workload readiness: %v %s", err, output)
	}
	uid, err := exec.Command("kubectl", "get", "pod", name, "-o", "jsonpath={.metadata.uid}").Output()
	if err != nil {
		t.Fatal(err)
	}
	// The receiver leaves TCP sockets in its accept queue so only the short
	// client records an owner. PCAP's sidecar includes final enriched rows even
	// for connections whose close-event log has not yet been emitted.
	dir := exportSession(t, "pcap", []string{"--duration", "7"}, nil)
	rows := readJSONL(t, filepath.Join(dir, "capture.pcap.connections.jsonl"))
	protocols := map[string]bool{}
	for _, row := range rows {
		k8s, _ := row["kubernetes"].(map[string]any)
		if k8s["pod_uid"] == string(uid) {
			if protocol, ok := row["protocol"].(string); ok {
				protocols[protocol] = true
			}
		}
	}
	if !protocols["TCP"] || !protocols["UDP"] {
		t.Fatalf("missing short-flow attribution for pod %s: %v", uid, protocols)
	}
	t.Logf("retained TCP and UDP attribution for pod %s (%s)", name, uid)
}

func TestExportCopyFailureRetainsRecoverablePod(t *testing.T) {
	image := exportImage(t)
	realKubectl, err := exec.LookPath("kubectl")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	// Fail only the evidence stream; every other command reaches Kubernetes.
	script := "#!/bin/sh\ncase \"$*\" in *'-- cat /evidence/'*) echo simulated-transfer-failure >&2; exit 1;; esac\nexec '" + strings.ReplaceAll(realKubectl, "'", "'\"'\"'") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pluginBin, "--image", image, "--output-dir", dir, "--output-format", "pcapng", "--", "--headless", "--output", "json", "--duration", "3", "--no-geoip")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "retained") {
		t.Fatalf("missing transfer error/recovery: %v %s", err, stderr.String())
	}
	var podName string
	for _, line := range strings.Split(stderr.String(), "\n") {
		if strings.HasPrefix(line, "Debug pod: ") {
			_, podName, _ = strings.Cut(strings.TrimPrefix(line, "Debug pod: "), "/")
		}
	}
	if podName == "" {
		t.Fatalf("missing pod name: %s", stderr.String())
	}
	t.Cleanup(func() { _ = exec.Command(realKubectl, "delete", "pod", podName, "--wait=true").Run() })
	if err := exec.Command(realKubectl, "get", "pod", podName).Run(); err != nil {
		t.Fatal("evidence pod was deleted")
	}
	recovered := filepath.Join(dir, "recovered.pcapng")
	if output, err := exec.Command(realKubectl, "cp", "-c", "rustnet-export", podName+":/evidence/capture.pcapng", recovered).CombinedOutput(); err != nil {
		t.Fatalf("recovery failed: %v %s", err, output)
	}
	checkPCAPNG(t, recovered)
}
