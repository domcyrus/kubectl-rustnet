package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var pluginBin string

// cleanupRustnetPods deletes only rustnet-debug pods (by label) and waits until they're gone.
func cleanupRustnetPods(t *testing.T) {
	t.Helper()
	_ = exec.Command("kubectl", "delete", "pods", "-n", "default",
		"-l", "app=rustnet-debug", "--force", "--grace-period=0").Run()
	for i := 0; i < 10; i++ {
		out, _ := exec.Command("kubectl", "get", "pods", "-n", "default",
			"-l", "app=rustnet-debug", "--no-headers", "-o", "name").Output()
		if strings.TrimSpace(string(out)) == "" {
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func TestMain(m *testing.M) {
	pluginBin = os.Getenv("KUBECTL_RUSTNET_BIN")
	if pluginBin == "" {
		pluginBin = "kubectl-rustnet"
	}

	// Resolve to absolute path so it works regardless of test working directory
	if !filepath.IsAbs(pluginBin) {
		abs, err := filepath.Abs(pluginBin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve plugin binary path %q: %v\n", pluginBin, err)
			os.Exit(1)
		}
		pluginBin = abs
	}

	// Verify the plugin binary exists
	if _, err := os.Stat(pluginBin); err != nil {
		fmt.Fprintf(os.Stderr, "plugin binary %q not found: %v\n", pluginBin, err)
		os.Exit(1)
	}

	// Verify kubectl is available and cluster is reachable
	if err := exec.Command("kubectl", "cluster-info").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "kubectl cluster-info failed: %v (is a kind cluster running?)\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func TestPluginCreatesAndCleansUpPod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run the plugin with a short timeout so it exits on its own
	cmd := exec.CommandContext(ctx, pluginBin, "--timeout", "3s", "--namespace", "default")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // May exit non-zero due to timeout or TTY issues in CI

	// Verify no rustnet-debug pods are left behind
	out, err := exec.Command("kubectl", "get", "pods", "-n", "default",
		"-l", "app=rustnet-debug", "--no-headers", "-o", "name").CombinedOutput()
	if err != nil {
		t.Logf("kubectl get pods stderr: %s", string(out))
	}

	pods := strings.TrimSpace(string(out))
	if pods != "" {
		t.Errorf("expected no leftover pods, found: %s", pods)
		cleanupRustnetPods(t)
	}
}

func TestPluginNodeSelector(t *testing.T) {
	// Get the node name from the kind cluster
	out, err := exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[0].metadata.name}").Output()
	if err != nil {
		t.Fatalf("failed to get node name: %v", err)
	}
	nodeName := strings.TrimSpace(string(out))
	if nodeName == "" {
		t.Fatal("no nodes found in cluster")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start the plugin in background so we can inspect the pod
	cmd := exec.CommandContext(ctx, pluginBin, "--node", nodeName, "--timeout", "10s", "--namespace", "default")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start plugin: %v", err)
	}

	// Wait for the pod to appear
	var podJSON []byte
	for i := 0; i < 15; i++ {
		time.Sleep(1 * time.Second)
		podJSON, err = exec.Command("kubectl", "get", "pods", "-n", "default",
			"--field-selector=status.phase!=Succeeded,status.phase!=Failed",
			"-o", "json").Output()
		if err == nil && strings.Contains(string(podJSON), "rustnet-debug") {
			break
		}
	}

	// Kill the plugin to trigger cleanup
	cancel()
	_ = cmd.Wait()

	if !strings.Contains(string(podJSON), "rustnet-debug") {
		t.Skip("pod did not appear in time (may be an image pull issue)")
		return
	}

	// Parse the pod spec to verify nodeSelector
	var podList struct {
		Items []struct {
			Spec struct {
				NodeSelector map[string]string `json:"nodeSelector"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(podJSON, &podList); err != nil {
		t.Fatalf("failed to parse pod JSON: %v", err)
	}

	found := false
	for _, p := range podList.Items {
		if v, ok := p.Spec.NodeSelector["kubernetes.io/hostname"]; ok && v == nodeName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected nodeSelector with hostname %s", nodeName)
	}

	cleanupRustnetPods(t)
}

func TestPluginPodOverrides(t *testing.T) {
	// Clean up any leftover rustnet-debug pods and wait until none remain
	cleanupRustnetPods(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start the plugin
	cmd := exec.CommandContext(ctx, pluginBin, "--timeout", "10s", "--namespace", "default",
		"--", "--no-dpi", "-i", "eth0")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start plugin: %v", err)
	}

	// Wait for the pod to appear
	var podJSON []byte
	var err error
	for i := 0; i < 15; i++ {
		time.Sleep(1 * time.Second)
		podJSON, err = exec.Command("kubectl", "get", "pods", "-n", "default",
			"--field-selector=status.phase!=Succeeded,status.phase!=Failed",
			"-o", "json").Output()
		if err == nil && strings.Contains(string(podJSON), "rustnet-debug") {
			break
		}
	}

	cancel()
	_ = cmd.Wait()

	if !strings.Contains(string(podJSON), "rustnet-debug") {
		t.Skip("pod did not appear in time (may be an image pull issue)")
		return
	}

	type podItem struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			HostNetwork bool `json:"hostNetwork"`
			HostPID     bool `json:"hostPID"`
			Containers  []struct {
				Args            []string `json:"args"`
				SecurityContext struct {
					RunAsUser    *int64 `json:"runAsUser"`
					Capabilities struct {
						Add []string `json:"add"`
					} `json:"capabilities"`
				} `json:"securityContext"`
			} `json:"containers"`
		} `json:"spec"`
	}
	var podList struct {
		Items []podItem `json:"items"`
	}
	if err := json.Unmarshal(podJSON, &podList); err != nil {
		t.Fatalf("failed to parse pod JSON: %v", err)
	}

	// Find the rustnet-debug pod
	var found *podItem
	for i, p := range podList.Items {
		if strings.HasPrefix(p.Metadata.Name, "rustnet-debug") {
			found = &podList.Items[i]
			break
		}
	}
	if found == nil {
		t.Skip("rustnet-debug pod not found in pod list")
		return
	}

	p := found
	if !p.Spec.HostNetwork {
		t.Error("expected hostNetwork=true")
	}
	if !p.Spec.HostPID {
		t.Error("expected hostPID=true")
	}
	if len(p.Spec.Containers) == 0 {
		t.Fatal("no containers found")
	}
	c := p.Spec.Containers[0]
	if c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != 0 {
		t.Error("expected runAsUser=0")
	}

	// Verify capabilities contain NET_RAW, BPF, PERFMON
	capSet := make(map[string]bool)
	for _, cap := range c.SecurityContext.Capabilities.Add {
		capSet[cap] = true
	}
	for _, expected := range []string{"NET_RAW", "BPF", "PERFMON"} {
		if !capSet[expected] {
			t.Errorf("expected capability %s", expected)
		}
	}

	// Verify rustnet args were passed
	expectedArgs := []string{"--no-dpi", "-i", "eth0"}
	if len(c.Args) != len(expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, c.Args)
	} else {
		for i, arg := range c.Args {
			if arg != expectedArgs[i] {
				t.Errorf("expected arg[%d]=%s, got %s", i, expectedArgs[i], arg)
			}
		}
	}

	cleanupRustnetPods(t)
}

func TestPluginCleanupOnInterrupt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pluginBin, "--timeout", "60s", "--namespace", "default")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start plugin: %v", err)
	}

	// Wait for the pod to appear
	podFound := false
	for i := 0; i < 15; i++ {
		time.Sleep(1 * time.Second)
		out, _ := exec.Command("kubectl", "get", "pods", "-n", "default",
			"-l", "app=rustnet-debug", "--no-headers", "-o", "name").Output()
		if strings.TrimSpace(string(out)) != "" {
			podFound = true
			break
		}
	}

	if !podFound {
		cancel()
		_ = cmd.Wait()
		t.Skip("pod did not appear in time")
		return
	}

	// Send SIGINT to the plugin process
	_ = cmd.Process.Signal(os.Interrupt)

	// Wait for the process to exit
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Error("plugin did not exit after SIGINT within 15s")
	}

	// Verify pod is cleaned up
	time.Sleep(2 * time.Second)
	out, _ := exec.Command("kubectl", "get", "pods", "-n", "default",
		"-l", "app=rustnet-debug", "--no-headers", "-o", "name").Output()
	pods := strings.TrimSpace(string(out))
	if pods != "" {
		t.Errorf("expected pod to be cleaned up after SIGINT, found: %s", pods)
		cleanupRustnetPods(t)
	}
}
