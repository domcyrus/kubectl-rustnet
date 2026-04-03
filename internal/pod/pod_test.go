package pod

import (
	"encoding/json"
	"testing"
)

func TestBuildOverridesDefault(t *testing.T) {
	opts := Options{
		Image: "ghcr.io/domcyrus/rustnet:latest",
	}
	raw, err := BuildOverrides(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	meta := result["metadata"].(map[string]any)
	labels := meta["labels"].(map[string]any)
	if labels["app"] != "rustnet-debug" {
		t.Errorf("expected label app=rustnet-debug, got %v", labels["app"])
	}

	spec := result["spec"].(map[string]any)
	if spec["hostNetwork"] != true {
		t.Error("expected hostNetwork=true")
	}
	if spec["hostPID"] != true {
		t.Error("expected hostPID=true")
	}
	if spec["restartPolicy"] != "Never" {
		t.Error("expected restartPolicy=Never")
	}
	if _, ok := spec["nodeSelector"]; ok {
		t.Error("expected no nodeSelector when node is empty")
	}

	containers := spec["containers"].([]any)
	c := containers[0].(map[string]any)
	if c["name"] != "rustnet-debug" {
		t.Errorf("expected container name rustnet-debug, got %v", c["name"])
	}
	if c["stdin"] != true || c["tty"] != true {
		t.Error("expected stdin=true, tty=true")
	}

	sc := c["securityContext"].(map[string]any)
	if sc["runAsUser"].(float64) != 0 {
		t.Error("expected runAsUser=0")
	}
	caps := sc["capabilities"].(map[string]any)["add"].([]any)
	expected := []string{"NET_RAW", "BPF", "PERFMON"}
	if len(caps) != len(expected) {
		t.Fatalf("expected %d capabilities, got %d", len(expected), len(caps))
	}
	for i, cap := range caps {
		if cap.(string) != expected[i] {
			t.Errorf("expected cap %s at index %d, got %s", expected[i], i, cap)
		}
	}
}

func TestBuildOverridesLegacyKernel(t *testing.T) {
	opts := Options{
		Image:        "ghcr.io/domcyrus/rustnet:latest",
		LegacyKernel: true,
	}
	raw, err := BuildOverrides(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	spec := result["spec"].(map[string]any)
	containers := spec["containers"].([]any)
	sc := containers[0].(map[string]any)["securityContext"].(map[string]any)
	caps := sc["capabilities"].(map[string]any)["add"].([]any)

	expected := []string{"NET_RAW", "SYS_ADMIN"}
	if len(caps) != len(expected) {
		t.Fatalf("expected %d capabilities, got %d", len(expected), len(caps))
	}
	for i, cap := range caps {
		if cap.(string) != expected[i] {
			t.Errorf("expected cap %s, got %s", expected[i], cap)
		}
	}
}

func TestBuildOverridesPrivileged(t *testing.T) {
	opts := Options{
		Image:      "ghcr.io/domcyrus/rustnet:latest",
		Privileged: true,
	}
	raw, err := BuildOverrides(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	spec := result["spec"].(map[string]any)
	containers := spec["containers"].([]any)
	sc := containers[0].(map[string]any)["securityContext"].(map[string]any)

	if sc["privileged"] != true {
		t.Error("expected privileged=true")
	}
	if _, ok := sc["capabilities"]; ok {
		t.Error("expected no capabilities when privileged")
	}
}

func TestBuildOverridesNodeSelector(t *testing.T) {
	opts := Options{
		Image: "ghcr.io/domcyrus/rustnet:latest",
		Node:  "worker-3",
	}
	raw, err := BuildOverrides(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	spec := result["spec"].(map[string]any)
	ns := spec["nodeSelector"].(map[string]any)
	if ns["kubernetes.io/hostname"] != "worker-3" {
		t.Errorf("expected node worker-3, got %v", ns["kubernetes.io/hostname"])
	}
}

func TestBuildOverridesRustnetArgs(t *testing.T) {
	opts := Options{
		Image:       "ghcr.io/domcyrus/rustnet:latest",
		RustnetArgs: []string{"-i", "eth0", "--no-dpi"},
	}
	raw, err := BuildOverrides(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	spec := result["spec"].(map[string]any)
	containers := spec["containers"].([]any)
	args := containers[0].(map[string]any)["args"].([]any)

	expected := []string{"-i", "eth0", "--no-dpi"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(args))
	}
	for i, arg := range args {
		if arg.(string) != expected[i] {
			t.Errorf("expected arg %s, got %s", expected[i], arg)
		}
	}
}

func TestBuildOverridesNoArgsOmitted(t *testing.T) {
	opts := Options{
		Image: "ghcr.io/domcyrus/rustnet:latest",
	}
	raw, err := BuildOverrides(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	spec := result["spec"].(map[string]any)
	containers := spec["containers"].([]any)
	if _, ok := containers[0].(map[string]any)["args"]; ok {
		t.Error("expected args to be omitted when empty")
	}
}
