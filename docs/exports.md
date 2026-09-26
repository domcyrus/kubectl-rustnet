<p align="center"><strong>English</strong> | <a href="exports.zh-CN.md">简体中文</a> | <a href="exports.ja.md">日本語</a></p>

# Saving capture evidence

Use `--output-dir` to copy completed captures to your machine before the debug pod is deleted:

```bash
kubectl rustnet --node worker-3 --timeout 5m --output-dir ./captures
kubectl rustnet --output-dir ./captures --output-format pcapng
```

The first command saves JSONL connection events and annotated PCAPNG packets. The second saves only PCAPNG. A headless session requires a RustNet image with headless support:

```bash
kubectl rustnet --output-dir ./captures --output-format pcap -- \
  --headless --duration 30 --no-geoip
```

Each run creates a new session directory, so earlier captures are not overwritten. Directories have mode `0700` and evidence files have mode `0600`. Choose a format with `--output-format`:

| Format | Files |
| --- | --- |
| `jsonl` | `connections.jsonl` with connection events |
| `pcapng` | `capture.pcapng` with annotated packets |
| `pcap` | `capture.pcap` and `capture.pcap.connections.jsonl` with final connection metadata |
| `both` (default) | `connections.jsonl` and `capture.pcapng` |

`--json-log`, `--pcap-export`, and `--pcapng-export` cannot be combined with `--output-dir`. Without `--output-dir`, those RustNet options still write inside the ephemeral pod, so copy the files before exiting.

## Permissions and recovery

The plugin needs `pods/exec` access for export, in addition to pod creation, attachment, and deletion. See the [sample RBAC policy](../deploy/rbac.yaml).

On normal exit, timeout, Ctrl+C, SIGTERM, or an attach failure, the plugin asks RustNet to stop gracefully and waits up to 60 seconds for its writers. A helper container keeps the shared `emptyDir` available while files are copied. The wrapper uses `CHOWN` to transfer file ownership; the helper drops all capabilities. The image must support graceful SIGINT/SIGTERM shutdown and include `/bin/sh`, `sleep`, `chown`, `touch`, `cat`, `mv`, and `sha256sum`. The official Debian-based image provides these tools.

The plugin streams each file locally and verifies its SHA-256 checksum before deleting the pod. Copying has a five-minute timeout and uses the capture namespace, context, and kubeconfig. If shutdown, copying, or verification fails, the command leaves the pod and any local `.partial` files in place and prints a `kubectl cp` recovery command. Recover the files, then delete the pod manually. `kubectl cp` needs `tar` in the helper image. The pod's `emptyDir` survives container exit, but not pod deletion or node loss.

JSONL events contain the metadata available when each event is emitted. For final metadata enrichment of connections still tracked at shutdown, choose `pcap` and its JSONL sidecar. Attributing processes that exit between Kubernetes scans requires an image containing [RustNet #634](https://github.com/domcyrus/rustnet/pull/634). See [issue #20](https://github.com/domcyrus/kubectl-rustnet/issues/20) for the coordinated export workflow.

## Integration tests

The export tests need a RustNet image with graceful signal shutdown and headless support. The short-flow test also needs a Python image. Load both images into a local cluster and use an isolated kubeconfig:

```bash
KUBECONFIG=/path/to/test-kubeconfig \
KUBECTL_RUSTNET_BIN="$PWD/kubectl-rustnet" \
RUSTNET_EXPORT_IMAGE=rustnet:export-test \
RUSTNET_EXPORT_WORKLOAD_IMAGE=python:3-slim \
go test ./e2e -run '^TestExport' -v -timeout 300s
```

The tests cover normal exit, timeout, SIGINT/SIGTERM, PCAP sidecars, recovery after a failed transfer, and short TCP/UDP processes in Kubernetes pods. They verify that local evidence remains readable after the debug pod is deleted.
