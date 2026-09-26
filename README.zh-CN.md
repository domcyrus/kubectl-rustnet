<p align="center"><img src="assets/rustnet.svg" alt="RustNet 标志" width="96" height="96"></p>

<h1 align="center">kubectl-rustnet</h1>

<p align="center"><a href="README.md">English</a> | <strong>简体中文</strong> | <a href="README.ja.md">日本語</a></p>

通过 `kubectl` 在 Kubernetes 节点上运行 [RustNet](https://github.com/domcyrus/rustnet)。插件会创建临时调试 Pod，打开网络监控界面，并在退出时删除该 Pod。

## 安装

```bash
kubectl krew install rustnet
```

也可以从[发布页面](https://github.com/domcyrus/kubectl-rustnet/releases)下载二进制文件，并将其放入 `PATH`。

## 运行

```bash
kubectl rustnet                         # 监控任意节点
kubectl rustnet --node worker-3         # 指定节点
kubectl rustnet -- -i eth0              # 向 RustNet 传递选项
```

默认抓取接口为 `any`，可查看节点各接口的流量，包括 Pod 的 veth 对端。官方镜像还可以将连接关联到 Pod 和容器。插件参数见 `kubectl rustnet --help`；界面操作和过滤方法见 [RustNet 使用指南](https://github.com/domcyrus/rustnet/blob/main/USAGE.zh-CN.md)。

## 保存抓包结果

`kubectl rustnet --output-dir ./captures --timeout 5m` 会在删除 Pod 前将 JSONL 和 PCAPNG 文件保存到本地。格式、权限和故障恢复方法见[导出指南](docs/exports.zh-CN.md)。

## 演示

<p align="center"><img src="assets/kubectl-rustnet.gif" alt="kubectl-rustnet 使用 RustNet v1.6.0 监控实时 Kubernetes 流量" width="800"></p>

使用插件在实时运行的 kind 集群中录制，RustNet 版本为 v1.6.0。

## 要求

需要集群权限，以创建和连接使用 `hostNetwork`、`hostPID`、只读主机 `/var/log` 挂载及抓包权限的 Pod。导出还需要 `pods/exec` 权限。集群必须能够拉取 `ghcr.io/domcyrus/rustnet:latest`。参见 [RBAC 示例](deploy/rbac.yaml)和 [RustNet Kubernetes 指南](https://github.com/domcyrus/rustnet/blob/main/USAGE.zh-CN.md#--kubernetes-mode-optional-feature)。

基于 [Apache 2.0](LICENSE) 许可证发布。
