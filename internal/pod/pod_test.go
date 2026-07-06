package pod

import (
	"encoding/json"
	"strings"
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

// When the user provides no rustnet args, kubectl-rustnet injects `-i any`
// so the pod captures from every interface (host + veth peers for inter-pod
// same-node traffic) rather than the default single interface.
func TestBuildOverridesDefaultsToAnyInterface(t *testing.T) {
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
	rawArgs, ok := containers[0].(map[string]any)["args"]
	if !ok {
		t.Fatal("expected args to be present with default -i any")
	}
	args := rawArgs.([]any)
	expected := []string{"-i", "any"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d (%v)", len(expected), len(args), args)
	}
	for i, a := range args {
		if a.(string) != expected[i] {
			t.Errorf("arg[%d] = %v, want %v", i, a, expected[i])
		}
	}
}

// The debug pod mounts the host's /var/log read-only so RustNet can resolve
// pod and container names from the kubelet log directories.
func TestBuildOverridesMountsVarLog(t *testing.T) {
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

	volsRaw, ok := spec["volumes"]
	if !ok {
		t.Fatal("expected spec.volumes to be present")
	}
	vols := volsRaw.([]any)
	v0 := vols[0].(map[string]any)
	if v0["name"] != "var-log" {
		t.Errorf("expected volume name var-log, got %v", v0["name"])
	}
	hp := v0["hostPath"].(map[string]any)
	if hp["path"] != "/var/log" {
		t.Errorf("expected hostPath /var/log, got %v", hp["path"])
	}

	containers := spec["containers"].([]any)
	c := containers[0].(map[string]any)
	mountsRaw, ok := c["volumeMounts"]
	if !ok {
		t.Fatal("expected container.volumeMounts to be present")
	}
	mounts := mountsRaw.([]any)
	m0 := mounts[0].(map[string]any)
	if m0["mountPath"] != "/var/log" {
		t.Errorf("expected mountPath /var/log, got %v", m0["mountPath"])
	}
	if m0["readOnly"] != true {
		t.Error("expected /var/log mount to be read-only")
	}
}

// Other user args alongside no interface flag: prepend `-i any` but preserve
// everything else verbatim.
func TestBuildOverridesPrependAnyWhenInterfaceNotSet(t *testing.T) {
	opts := Options{
		Image:       "ghcr.io/domcyrus/rustnet:latest",
		RustnetArgs: []string{"--no-dpi", "--json-log", "/tmp/r.jsonl"},
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
	expected := []string{"-i", "any", "--no-dpi", "--json-log", "/tmp/r.jsonl"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d (%v)", len(expected), len(args), args)
	}
	for i, a := range args {
		if a.(string) != expected[i] {
			t.Errorf("arg[%d] = %v, want %v", i, a, expected[i])
		}
	}
}

// Every recognised form of an explicit interface flag must suppress the
// `-i any` injection.
func TestBuildOverridesUserInterfaceWinsOverDefault(t *testing.T) {
	cases := [][]string{
		{"-i", "eth0"},
		{"-i=eth0"},
		{"--interface", "eth0"},
		{"--interface=eth0"},
		{"-ieth0"},
	}
	for _, userArgs := range cases {
		t.Run(strings.Join(userArgs, " "), func(t *testing.T) {
			opts := Options{
				Image:       "ghcr.io/domcyrus/rustnet:latest",
				RustnetArgs: append([]string{}, userArgs...),
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
			if len(args) != len(userArgs) {
				t.Fatalf("expected %d args (no injection), got %d (%v)", len(userArgs), len(args), args)
			}
			for i, a := range args {
				if a.(string) != userArgs[i] {
					t.Errorf("arg[%d] = %v, want %v", i, a, userArgs[i])
				}
			}
		})
	}
}
