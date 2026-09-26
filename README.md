# kubectl-rustnet

A [kubectl plugin](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/) that runs [RustNet](https://github.com/domcyrus/rustnet) as an ephemeral debug pod on Kubernetes nodes for real-time network monitoring.

![kubectl-rustnet Demo](./assets/kubectl-rustnet.gif)

## Features

- Deploys RustNet with the correct security context for packet capture and eBPF
- Interactive TUI with deep packet inspection for 15+ protocols
- Process-to-connection attribution via eBPF on the target node
- Pod and container attribution: connections are labeled with their pod, namespace, and container, and can be filtered with the `pod:`, `ns:`, and `container:` keywords (see the [RustNet usage guide](https://github.com/domcyrus/rustnet/blob/main/USAGE.md#--kubernetes-mode-optional-feature))
- Automatic cleanup of debug pods on exit
- Node targeting via `--node` flag

## Installation

### Via Krew

```bash
kubectl krew install rustnet
```

### Manual

Download the binary from the [releases page](https://github.com/domcyrus/kubectl-rustnet/releases) and place it in your `$PATH`.

## Prerequisites

- `kubectl` configured with cluster access
- Cluster permissions to create pods with `hostNetwork`, `hostPID`, and elevated capabilities
- The `ghcr.io/domcyrus/rustnet` image accessible from the cluster

### RBAC

The plugin needs `pods/exec` access for evidence export in addition to pod creation, attachment, and deletion.

A sample ClusterRole is provided in [`deploy/rbac.yaml`](deploy/rbac.yaml). Apply it and bind to your user:

```bash
kubectl apply -f deploy/rbac.yaml
```

## Usage

```bash
# Monitor any node (scheduler picks). RustNet captures from every interface
# by default (-i any), which includes the host-side veth peers used by
# pod-to-pod same-node communication.
kubectl rustnet

# Monitor a specific node
kubectl rustnet --node worker-3

# In a specific namespace with a timeout
kubectl rustnet -n monitoring --timeout 5m

# Pin the capture to a single interface (overrides the -i any default)
kubectl rustnet -- -i eth0 --no-dpi

# Use a specific image tag. Note: pod/container attribution needs a tag
# newer than v1.4.0 (or latest); older tags fall back to plain monitoring.
kubectl rustnet --image ghcr.io/domcyrus/rustnet:v1.1.0

# Legacy kernels (< 5.8) that don't support CAP_BPF
kubectl rustnet --legacy-kernel

# Privileged mode (when fine-grained caps aren't enough)
kubectl rustnet --privileged
```

### Plugin Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--namespace`, `-n` | `default` | Kubernetes namespace |
| `--node` | (any) | Target a specific node |
| `--image` | `ghcr.io/domcyrus/rustnet:latest` | Container image (has pod/container attribution enabled) |
| `--output-dir` | (disabled) | Save exports to a private local session directory before deleting the pod |
| `--output-format` | `both` | With `--output-dir`: `jsonl`, `pcapng`, `pcap` (with JSONL sidecar), or `both` (JSONL + PCAPNG) |
| `--timeout` | 0 (none) | Session timeout (e.g. `5m`, `1h`) |
| `--privileged` | false | Run in privileged mode |
| `--legacy-kernel` | false | Use SYS_ADMIN instead of BPF+PERFMON |
| `--kubeconfig` | (default) | Path to kubeconfig file |
| `--context` | (default) | Kubernetes context |

### RustNet Flags (after `--`)

| Flag | Description |
|------|-------------|
| `-i`, `--interface` | Network interface to monitor (defaults to `any` when not set, so inter-pod same-node traffic on host-side veths is captured) |
| `-f`, `--bpf-filter` | BPF filter expression |
| `--no-dpi` | Disable deep packet inspection |
| `--resolve-dns` | Enable reverse DNS lookups |
| `--no-geoip` | Disable GeoIP lookups |
| `--json-log FILE` | Export connection events as JSON (includes pod/container attribution) |
| `--pcap-export FILE` | Export packets to PCAP file with a JSONL sidecar |
| `--pcapng-export FILE` | Export packets to an annotated PCAPNG with per-packet process comments |
| `--kubernetes MODE` | Pod/container attribution: `auto` (default, on inside a pod), `on`, or `off` |
| `--refresh-interval MS` | UI refresh interval (default: 1000) |
| `--no-color` | Disable colors |

### Save capture evidence

```bash
# Collect connection events and annotated packets, then copy before cleanup
kubectl rustnet --node worker-3 --timeout 5m --output-dir ./captures

# Save only annotated PCAPNG
kubectl rustnet --output-dir ./captures --output-format pcapng

# Headless session on a RustNet image with headless support
kubectl rustnet --output-dir ./captures --output-format pcap -- \
  --headless --duration 30 --no-geoip
```

`--output-dir` creates a unique session subdirectory without overwriting previous
captures. Session directories have mode `0700` and evidence files have mode `0600`.
The formats produce:

| Format | Files |
| --- | --- |
| `jsonl` | `connections.jsonl` (RustNet connection events) |
| `pcapng` | `capture.pcapng` (annotated packets) |
| `pcap` | `capture.pcap` and `capture.pcap.connections.jsonl` (final connection metadata) |
| `both` (default) | `connections.jsonl` and `capture.pcapng` |

Use `--output-format` to select these exports. Passing `--json-log`,
`--pcap-export`, or `--pcapng-export` alongside `--output-dir` is rejected before
creating a pod. Without `--output-dir`, those RustNet arguments retain their
existing behavior: copy files manually before quitting the debug session.

On normal exit, timeout, Ctrl+C, SIGTERM, or an attach failure, the plugin requests
a graceful RustNet stop and waits up to 60 seconds for its writers to finish. A
helper container using the same image keeps an `emptyDir` volume available after
RustNet exits. The capture wrapper transfers ownership of the completed files
using `CHOWN`; the helper drops all capabilities. The image must provide
`/bin/sh`, `sleep`, `chown`, `touch`, `cat`, `mv`, and `sha256sum`, as the official
Debian-based image does. RustNet must support graceful SIGINT/SIGTERM shutdown.

Each selected file is streamed locally and verified against its remote SHA-256
checksum before pod deletion. The copy phase has a five-minute timeout and uses
the same namespace, context, and kubeconfig as capture. If shutdown, copying, or
verification fails, the command returns an error, retains the pod and any local
`.partial` files, and prints a `kubectl cp` recovery command. After recovery,
delete the retained pod manually. `kubectl cp` requires `tar` in the helper image.
The pod's `emptyDir` survives container exit but not pod deletion or node loss.

JSONL event records reflect the metadata available when each event is emitted.
For final enrichment of connections still tracked at shutdown, use `pcap` and
its JSONL sidecar. Kubernetes attribution of processes that exit between scans
requires an image containing [RustNet #634](https://github.com/domcyrus/rustnet/pull/634).
See [issue #20](https://github.com/domcyrus/kubectl-rustnet/issues/20) for the
coordinated export workflow.


## How It Works

The plugin creates an ephemeral pod with:

- **`hostNetwork: true`** for node-level network visibility
- **`hostPID: true`** for process attribution via eBPF
- **`runAsUser: 0`** to read host `/proc` entries for process lookup
- **`NET_RAW` + `BPF` + `PERFMON`** capabilities for packet capture and eBPF
- **Read-only `/var/log` mount** so RustNet can resolve pod and container names from the kubelet log directories (`/var/log/containers`, `/var/log/pods`)

On exit (or Ctrl+C), the pod is automatically deleted. With `--output-dir`, deletion happens only after verified evidence download.

## Development

```bash
# Build
go build -o kubectl-rustnet ./cmd/kubectl-rustnet

# Unit tests
go test ./internal/... -v

# E2E tests (requires kind and Docker)
./e2e/setup.sh create
KUBECTL_RUSTNET_BIN=./kubectl-rustnet go test ./e2e/ -v -timeout 300s
./e2e/setup.sh delete
```

### Export integration tests

The export tests require a RustNet image with graceful signal shutdown and
headless support. The short-flow test additionally requires a Python image.
Load both images into your local cluster first, then use an isolated kubeconfig:

```bash
KUBECONFIG=/path/to/test-kubeconfig \
KUBECTL_RUSTNET_BIN="$PWD/kubectl-rustnet" \
RUSTNET_EXPORT_IMAGE=rustnet:export-test \
RUSTNET_EXPORT_WORKLOAD_IMAGE=python:3-slim \
go test ./e2e -run '^TestExport' -v -timeout 300s
```

These tests cover normal exit, timeout, SIGINT/SIGTERM, PCAP sidecars, recovery
after a failed transfer, and short TCP/UDP processes in Kubernetes pods. They
check that the local evidence remains readable after the debug pod is deleted.

## License

Apache License 2.0. See [LICENSE](LICENSE).
