package pod

import (
	"encoding/json"
	"fmt"
	"strings"
)

// hasInterfaceFlag reports whether the user-supplied rustnet args already pin
// a capture interface via `-i` / `--interface` (with space or `=` separator)
// or the glued short forms `-iNAME` / `-i=NAME`. Used to decide whether
// kubectl-rustnet should inject `-i any` as a default.
func hasInterfaceFlag(args []string) bool {
	for _, a := range args {
		switch {
		case a == "-i", a == "--interface":
			return true
		case strings.HasPrefix(a, "--interface="):
			return true
		case strings.HasPrefix(a, "-i") && len(a) > 2:
			return true
		}
	}
	return false
}

// Options configures how the debug pod is constructed.
type Options struct {
	Image        string
	Node         string
	Privileged   bool
	LegacyKernel bool
	RustnetArgs  []string
	ExportFormat string
}

// Label is the fixed label applied to all rustnet debug pods for easy identification and cleanup.
const Label = "app=rustnet-debug"

type podOverride struct {
	APIVersion string          `json:"apiVersion"`
	Metadata   podOverrideMeta `json:"metadata"`
	Spec       podOverrideSpec `json:"spec"`
}

type podOverrideMeta struct {
	Labels map[string]string `json:"labels"`
}

type podOverrideSpec struct {
	HostNetwork   bool                `json:"hostNetwork"`
	HostPID       bool                `json:"hostPID"`
	NodeSelector  map[string]string   `json:"nodeSelector,omitempty"`
	Containers    []containerOverride `json:"containers"`
	Volumes       []volume            `json:"volumes,omitempty"`
	RestartPolicy string              `json:"restartPolicy"`
}

type containerOverride struct {
	Name            string          `json:"name"`
	Image           string          `json:"image"`
	Command         []string        `json:"command,omitempty"`
	Args            []string        `json:"args,omitempty"`
	Stdin           bool            `json:"stdin"`
	TTY             bool            `json:"tty"`
	VolumeMounts    []volumeMount   `json:"volumeMounts,omitempty"`
	SecurityContext securityContext `json:"securityContext"`
}

type securityContext struct {
	Privileged               *bool         `json:"privileged,omitempty"`
	RunAsUser                *int64        `json:"runAsUser,omitempty"`
	AllowPrivilegeEscalation *bool         `json:"allowPrivilegeEscalation,omitempty"`
	Capabilities             *capabilities `json:"capabilities,omitempty"`
}

type capabilities struct {
	Add  []string `json:"add,omitempty"`
	Drop []string `json:"drop,omitempty"`
}

type volume struct {
	Name     string    `json:"name"`
	HostPath *hostPath `json:"hostPath,omitempty"`
	EmptyDir *struct{} `json:"emptyDir,omitempty"`
}

type hostPath struct {
	Path string `json:"path"`
	Type string `json:"type,omitempty"`
}

type volumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
	ReadOnly  bool   `json:"readOnly"`
}

// BuildOverrides constructs the JSON override string for kubectl run.
func BuildOverrides(opts Options) (string, error) {
	sc := securityContext{}
	uid := int64(0)
	sc.RunAsUser = &uid

	if opts.Privileged {
		priv := true
		sc.Privileged = &priv
	} else {
		caps := []string{"NET_RAW"}
		if opts.LegacyKernel {
			caps = append(caps, "SYS_ADMIN")
		} else {
			caps = append(caps, "BPF", "PERFMON")
		}
		if opts.ExportFormat != "" {
			caps = append(caps, "CHOWN")
		}
		sc.Capabilities = &capabilities{Add: caps}
	}

	container := containerOverride{
		Name:  "rustnet-debug",
		Image: opts.Image,
		Stdin: true,
		TTY:   true,
		// Mount the kubelet log directories read-only so RustNet can resolve
		// pod and container names from /var/log/{containers,pods}. These are
		// kubelet-managed and runtime-agnostic. RustNet degrades to pod UID +
		// container ID if it can't read them.
		VolumeMounts: []volumeMount{
			{Name: "var-log", MountPath: "/var/log", ReadOnly: true},
		},
		SecurityContext: sc,
	}
	// Default rustnet to `-i any` so we see traffic on every interface,
	// including the host-side veth peers used by inter-pod same-node
	// communication. Skip when the user has already pinned an interface.
	args, err := exportArgs(opts.ExportFormat, opts.RustnetArgs)
	if err != nil {
		return "", err
	}
	if !hasInterfaceFlag(args) {
		args = append([]string{"-i", "any"}, args...)
	}
	container.Args = args

	spec := podOverrideSpec{
		HostNetwork: true,
		HostPID:     true,
		Containers:  []containerOverride{container},
		Volumes: []volume{
			{Name: "var-log", HostPath: &hostPath{Path: "/var/log", Type: "Directory"}},
		},
		RestartPolicy: "Never",
	}
	if opts.ExportFormat != "" {
		spec.Volumes = append(spec.Volumes, volume{Name: "evidence", EmptyDir: &struct{}{}})
		spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, volumeMount{Name: "evidence", MountPath: ExportPath})
		spec.Containers[0].Command = []string{"/bin/sh", "-c", captureScript, "rustnet"}
		noEscalation := false
		spec.Containers = append(spec.Containers, containerOverride{
			Name: ExportContainer, Image: opts.Image,
			Command:         []string{"/bin/sh", "-c", helperScript},
			VolumeMounts:    []volumeMount{{Name: "evidence", MountPath: ExportPath}},
			SecurityContext: securityContext{RunAsUser: &uid, AllowPrivilegeEscalation: &noEscalation, Capabilities: &capabilities{Drop: []string{"ALL"}}},
		})
	}
	for _, arg := range opts.RustnetArgs {
		if arg == "--headless" {
			spec.Containers[0].Stdin = false
			spec.Containers[0].TTY = false
		}
	}
	if opts.Node != "" {
		spec.NodeSelector = map[string]string{
			"kubernetes.io/hostname": opts.Node,
		}
	}

	override := podOverride{
		APIVersion: "v1",
		Metadata: podOverrideMeta{
			Labels: map[string]string{
				"app": "rustnet-debug",
			},
		},
		Spec: spec,
	}

	data, err := json.Marshal(override)
	if err != nil {
		return "", fmt.Errorf("failed to marshal pod overrides: %w", err)
	}
	return string(data), nil
}
