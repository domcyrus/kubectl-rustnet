# kubectl-rustnet

A [kubectl plugin](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/) that runs [RustNet](https://github.com/domcyrus/rustnet) as an ephemeral debug pod on Kubernetes nodes for real-time network monitoring.

## Features

- Deploys RustNet with the correct security context for packet capture and eBPF
- Interactive TUI with deep packet inspection for 15+ protocols
- Process-to-connection attribution via eBPF on the target node
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

A sample ClusterRole is provided in [`deploy/rbac.yaml`](deploy/rbac.yaml). Apply it and bind to your user:

```bash
kubectl apply -f deploy/rbac.yaml
```

## Usage

```bash
# Monitor any node (scheduler picks)
kubectl rustnet

# Monitor a specific node
kubectl rustnet --node worker-3

# In a specific namespace with a timeout
kubectl rustnet -n monitoring --timeout 5m

# Pass flags to RustNet (after --)
kubectl rustnet -- -i eth0 --no-dpi

# Use a specific image tag
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
| `--image` | `ghcr.io/domcyrus/rustnet:latest` | Container image |
| `--timeout` | 0 (none) | Session timeout (e.g. `5m`, `1h`) |
| `--privileged` | false | Run in privileged mode |
| `--legacy-kernel` | false | Use SYS_ADMIN instead of BPF+PERFMON |
| `--kubeconfig` | (default) | Path to kubeconfig file |
| `--context` | (default) | Kubernetes context |

### RustNet Flags (after `--`)

| Flag | Description |
|------|-------------|
| `-i`, `--interface` | Network interface to monitor |
| `-f`, `--bpf-filter` | BPF filter expression |
| `--no-dpi` | Disable deep packet inspection |
| `--resolve-dns` | Enable reverse DNS lookups |
| `--no-geoip` | Disable GeoIP lookups |
| `--json-log FILE` | Export connection events as JSON |
| `--pcap-export FILE` | Export packets to PCAP file |
| `--refresh-interval MS` | UI refresh interval (default: 1000) |
| `--no-color` | Disable colors |

## How It Works

The plugin creates an ephemeral pod with:

- **`hostNetwork: true`** for node-level network visibility
- **`hostPID: true`** for process attribution via eBPF
- **`runAsUser: 0`** to read host `/proc` entries for process lookup
- **`NET_RAW` + `BPF` + `PERFMON`** capabilities for packet capture and eBPF

On exit (or Ctrl+C), the pod is automatically deleted.

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

## License

Apache License 2.0. See [LICENSE](LICENSE).
