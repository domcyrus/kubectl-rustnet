package runner

import (
	"strings"
	"testing"

	"github.com/domcyrus/kubectl-rustnet/internal/pod"
)

func TestBuildArgsDefault(t *testing.T) {
	opts := Options{
		Namespace: "default",
		Pod: pod.Options{
			Image: "ghcr.io/domcyrus/rustnet:latest",
		},
	}
	podName, args, err := BuildArgs(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(podName, "rustnet-debug-") {
		t.Errorf("expected pod name prefix rustnet-debug-, got %s", podName)
	}

	// Verify key args are present
	argStr := strings.Join(args, " ")
	for _, expected := range []string{"run", "--image", "--restart=Never", "--overrides", "--namespace", "default"} {
		if !strings.Contains(argStr, expected) {
			t.Errorf("expected args to contain %q", expected)
		}
	}
}

func TestBuildArgsWithKubeconfig(t *testing.T) {
	opts := Options{
		Namespace:  "monitoring",
		Kubeconfig: "/home/user/.kube/config",
		Context:    "prod-cluster",
		Pod: pod.Options{
			Image: "ghcr.io/domcyrus/rustnet:v1.0.0",
		},
	}
	_, args, err := BuildArgs(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "--kubeconfig /home/user/.kube/config") {
		t.Error("expected kubeconfig flag")
	}
	if !strings.Contains(argStr, "--context prod-cluster") {
		t.Error("expected context flag")
	}
}

func TestBuildArgsWithRustnetArgs(t *testing.T) {
	opts := Options{
		Namespace: "default",
		Pod: pod.Options{
			Image:       "ghcr.io/domcyrus/rustnet:latest",
			RustnetArgs: []string{"-i", "eth0", "--no-dpi"},
		},
	}
	_, args, err := BuildArgs(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Rustnet args should be in the overrides JSON, not after --
	for _, arg := range args {
		if arg == "--" {
			t.Error("unexpected -- separator; rustnet args should be in overrides")
		}
	}

	// Verify overrides contain the args
	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, `"--no-dpi"`) {
		// The args are inside the --overrides JSON value
		overridesIdx := -1
		for i, arg := range args {
			if arg == "--overrides" && i+1 < len(args) {
				overridesIdx = i + 1
				break
			}
		}
		if overridesIdx == -1 {
			t.Fatal("expected --overrides flag")
		}
		overrides := args[overridesIdx]
		if !strings.Contains(overrides, "--no-dpi") {
			t.Errorf("expected overrides to contain --no-dpi, got: %s", overrides)
		}
		if !strings.Contains(overrides, "eth0") {
			t.Errorf("expected overrides to contain eth0, got: %s", overrides)
		}
	}
}

func TestRandomSuffix(t *testing.T) {
	s1 := randomSuffix(8)
	s2 := randomSuffix(8)
	if len(s1) != 8 {
		t.Errorf("expected length 8, got %d", len(s1))
	}
	if s1 == s2 {
		t.Error("expected different random suffixes")
	}
}
