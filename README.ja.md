<p align="center"><img src="assets/rustnet.svg" alt="RustNet ロゴ" width="96" height="96"></p>

<h1 align="center">kubectl-rustnet</h1>

<p align="center"><a href="README.md">English</a> | <a href="README.zh-CN.md">简体中文</a> | <strong>日本語</strong></p>

`kubectl` から Kubernetes ノードで [RustNet](https://github.com/domcyrus/rustnet) を実行します。プラグインは一時的なデバッグ Pod を作成してネットワークモニターを開き、終了時に Pod を削除します。

## インストール

```bash
kubectl krew install rustnet
```

[リリースページ](https://github.com/domcyrus/kubectl-rustnet/releases)からバイナリをダウンロードし、`PATH` に配置することもできます。

## 実行

```bash
kubectl rustnet                         # 任意のノードを監視
kubectl rustnet --node worker-3         # ノードを指定
kubectl rustnet -- -i eth0              # RustNet にオプションを渡す
```

既定のキャプチャインターフェースは `any` で、Pod の veth 対向を含むノードの各インターフェースの通信を表示します。公式イメージでは接続を Pod やコンテナに関連付けることもできます。プラグインのフラグは `kubectl rustnet --help`、画面操作とフィルターは [RustNet 使用ガイド](https://github.com/domcyrus/rustnet/blob/main/USAGE.md)を参照してください。

## キャプチャの保存

`kubectl rustnet --output-dir ./captures --timeout 5m` は、Pod を削除する前に JSONL と PCAPNG ファイルをローカルに保存します。形式、権限、障害時の復旧方法は[エクスポートガイド](docs/exports.ja.md)を参照してください。

## デモ

<p align="center"><img src="assets/kubectl-rustnet.gif" alt="kubectl-rustnet と RustNet v1.6.0 で Kubernetes の通信を監視" width="800"></p>

稼働中の kind クラスタでプラグインを使って収録しました。RustNet のバージョンは v1.6.0 です。

## 必要な権限

`hostNetwork`、`hostPID`、ホストの `/var/log` の読み取り専用マウント、パケットキャプチャ用の権限を使う Pod を作成し、接続できるクラスタ権限が必要です。エクスポートには `pods/exec` 権限も必要です。クラスタから `ghcr.io/domcyrus/rustnet:latest` を取得できる必要があります。[RBAC の例](deploy/rbac.yaml)と [RustNet の Kubernetes ガイド](https://github.com/domcyrus/rustnet/blob/main/USAGE.md#--kubernetes-mode-optional-feature)を参照してください。

[Apache 2.0](LICENSE) ライセンスで公開しています。
