package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/domcyrus/kubectl-rustnet/internal/pod"
	"github.com/domcyrus/kubectl-rustnet/internal/runner"
)

var version = "dev"

const defaultImage = "ghcr.io/domcyrus/rustnet:latest"

func main() {
	// Plugin flags
	namespace := flag.String("namespace", "default", "Kubernetes namespace")
	flag.StringVar(namespace, "n", "default", "Kubernetes namespace (shorthand)")
	node := flag.String("node", "", "Target a specific node")
	image := flag.String("image", defaultImage, "Container image")
	privileged := flag.Bool("privileged", false, "Run in privileged mode")
	legacyKernel := flag.Bool("legacy-kernel", false, "Use SYS_ADMIN instead of BPF+PERFMON (for kernels < 5.8)")
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig file")
	context := flag.String("context", "", "Kubernetes context")
	timeout := flag.Duration("timeout", 0, "Timeout for the debug session (e.g. 5m, 1h). 0 means no timeout")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "v", false, "Print version and exit (shorthand)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `kubectl-rustnet - Run RustNet network monitor as a debug pod on Kubernetes nodes

Usage:
  kubectl rustnet [flags] [-- rustnet-args...]

Examples:
  kubectl rustnet                                  # Monitor any node
  kubectl rustnet --node worker-3                  # Monitor a specific node
  kubectl rustnet -n monitoring --timeout 5m       # In namespace with timeout
  kubectl rustnet -- -i eth0 --no-dpi              # Pass flags to RustNet

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
RustNet flags (after --):
  -i, --interface INTERFACE    Network interface to monitor
  -f, --bpf-filter FILTER      BPF filter expression
  --no-dpi                     Disable deep packet inspection
  --resolve-dns                Enable reverse DNS lookups
  --no-geoip                   Disable GeoIP lookups
  --json-log FILE              Export connection events as JSON
  --pcap-export FILE           Export packets to PCAP file
  --refresh-interval MS        UI refresh interval (default: 1000)
  --no-color                   Disable colors
`)
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("kubectl-rustnet %s\n", version)
		os.Exit(0)
	}

	// Everything after flag parsing is treated as rustnet args.
	// Go's flag package stops at the first non-flag or at --.
	rustnetArgs := flag.Args()

	opts := runner.Options{
		Namespace:  *namespace,
		Kubeconfig: *kubeconfig,
		Context:    *context,
		Timeout:    *timeout,
		Pod: pod.Options{
			Image:        *image,
			Node:         *node,
			Privileged:   *privileged,
			LegacyKernel: *legacyKernel,
			RustnetArgs:  rustnetArgs,
		},
	}

	if err := runner.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
