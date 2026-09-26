<p align="center"><a href="exports.md">English</a> | <strong>简体中文</strong> | <a href="exports.ja.md">日本語</a></p>

# 保存抓包结果

使用 `--output-dir` 在删除调试 Pod 前，将完整的抓包结果复制到本机：

```bash
kubectl rustnet --node worker-3 --timeout 5m --output-dir ./captures
kubectl rustnet --output-dir ./captures --output-format pcapng
```

第一条命令保存 JSONL 连接事件和带注释的 PCAPNG 数据包。第二条命令只保存 PCAPNG。无界面模式需要支持 headless 的 RustNet 镜像：

```bash
kubectl rustnet --output-dir ./captures --output-format pcap -- \
  --headless --duration 30 --no-geoip
```

每次运行都会创建新的会话目录，不会覆盖以前的抓包结果。目录权限为 `0700`，文件权限为 `0600`。通过 `--output-format` 选择格式：

| 格式 | 文件 |
| --- | --- |
| `jsonl` | 包含连接事件的 `connections.jsonl` |
| `pcapng` | 包含带注释数据包的 `capture.pcapng` |
| `pcap` | `capture.pcap` 和包含最终连接元数据的 `capture.pcap.connections.jsonl` |
| `both`（默认） | `connections.jsonl` 和 `capture.pcapng` |

`--json-log`、`--pcap-export` 和 `--pcapng-export` 不能与 `--output-dir` 一起使用。不使用 `--output-dir` 时，这些 RustNet 选项仍会将文件写入临时 Pod，必须在退出前复制文件。

## 权限和故障恢复

导出需要 `pods/exec` 权限，此外还需要创建、连接和删除 Pod 的权限。参见 [RBAC 示例](../deploy/rbac.yaml)。

正常退出、超时、Ctrl+C、SIGTERM 或连接失败时，插件会请求 RustNet 正常停止，并最多等待 60 秒，让写入操作完成。辅助容器会在复制文件期间保留共享的 `emptyDir`。包装程序使用 `CHOWN` 转移文件所有权；辅助容器会移除全部 capabilities。镜像必须支持通过 SIGINT/SIGTERM 正常停止，并提供 `/bin/sh`、`sleep`、`chown`、`touch`、`cat`、`mv` 和 `sha256sum`。官方基于 Debian 的镜像提供这些工具。

插件将每个文件传输到本机，验证其 SHA-256 校验和后才删除 Pod。复制操作的超时时间为五分钟，并使用抓包时的 namespace、context 和 kubeconfig。如果停止、复制或验证失败，插件会保留 Pod 和本地的 `.partial` 文件，并输出用于恢复的 `kubectl cp` 命令。恢复文件后请手动删除 Pod。`kubectl cp` 要求辅助容器镜像提供 `tar`。Pod 中的 `emptyDir` 在容器退出后仍然存在，但删除 Pod 或节点丢失后就不再可用。

JSONL 事件仅包含事件产生时可获取的元数据。如果需要在停止时补全仍受跟踪连接的最终元数据，请选择 `pcap` 及其 JSONL 附属文件。对于 Kubernetes 扫描间隔内退出的进程，需要包含 [RustNet #634](https://github.com/domcyrus/rustnet/pull/634) 的镜像才能关联 Pod 信息。参见[问题 #20](https://github.com/domcyrus/kubectl-rustnet/issues/20)了解协调导出流程。

## 集成测试

导出测试需要支持优雅关闭和 headless 模式的 RustNet 镜像。短连接测试还需要 Python 镜像。先将两个镜像加载到本地集群，再使用独立的 kubeconfig：

```bash
KUBECONFIG=/path/to/test-kubeconfig \
KUBECTL_RUSTNET_BIN="$PWD/kubectl-rustnet" \
RUSTNET_EXPORT_IMAGE=rustnet:export-test \
RUSTNET_EXPORT_WORKLOAD_IMAGE=python:3-slim \
go test ./e2e -run '^TestExport' -v -timeout 300s
```

测试覆盖正常退出、超时、SIGINT/SIGTERM、PCAP 附属文件、传输失败后的恢复，以及 Kubernetes Pod 中的短 TCP/UDP 连接。测试还会验证调试 Pod 删除后，本地抓包结果仍可读取。
