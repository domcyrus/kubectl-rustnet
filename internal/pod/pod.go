package pod

import (
	"encoding/json"
	"fmt"
)

// Options configures how the debug pod is constructed.
type Options struct {
	Image        string
	Node         string
	Privileged   bool
	LegacyKernel bool
	RustnetArgs  []string
}

// Label is the fixed label applied to all rustnet debug pods for easy identification and cleanup.
const Label = "app=rustnet-debug"

type podOverride struct {
	APIVersion string            `json:"apiVersion"`
	Metadata   podOverrideMeta   `json:"metadata"`
	Spec       podOverrideSpec   `json:"spec"`
}

type podOverrideMeta struct {
	Labels map[string]string `json:"labels"`
}

type podOverrideSpec struct {
	HostNetwork  bool                    `json:"hostNetwork"`
	HostPID      bool                    `json:"hostPID"`
	NodeSelector map[string]string       `json:"nodeSelector,omitempty"`
	Containers   []containerOverride     `json:"containers"`
	RestartPolicy string                 `json:"restartPolicy"`
}

type containerOverride struct {
	Name            string          `json:"name"`
	Image           string          `json:"image"`
	Args            []string        `json:"args,omitempty"`
	Stdin           bool            `json:"stdin"`
	TTY             bool            `json:"tty"`
	SecurityContext securityContext  `json:"securityContext"`
}

type securityContext struct {
	Privileged   *bool         `json:"privileged,omitempty"`
	RunAsUser    *int64        `json:"runAsUser,omitempty"`
	Capabilities *capabilities `json:"capabilities,omitempty"`
}

type capabilities struct {
	Add []string `json:"add"`
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
		sc.Capabilities = &capabilities{Add: caps}
	}

	container := containerOverride{
		Name:            "rustnet-debug",
		Image:           opts.Image,
		Stdin:           true,
		TTY:             true,
		SecurityContext: sc,
	}
	if len(opts.RustnetArgs) > 0 {
		container.Args = opts.RustnetArgs
	}

	spec := podOverrideSpec{
		HostNetwork:   true,
		HostPID:       true,
		Containers:    []containerOverride{container},
		RestartPolicy: "Never",
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
