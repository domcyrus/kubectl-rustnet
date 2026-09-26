<p align="center"><a href="exports.md">English</a> | <a href="exports.zh-CN.md">简体中文</a> | <strong>日本語</strong></p>

# キャプチャの保存

`--output-dir` を使うと、デバッグ Pod を削除する前に完成したキャプチャをローカルにコピーできます。

```bash
kubectl rustnet --node worker-3 --timeout 5m --output-dir ./captures
kubectl rustnet --output-dir ./captures --output-format pcapng
```

最初のコマンドは JSONL の接続イベントと注釈付き PCAPNG パケットを保存します。2 番目のコマンドは PCAPNG のみを保存します。ヘッドレス実行には、headless に対応した RustNet イメージが必要です。

```bash
kubectl rustnet --output-dir ./captures --output-format pcap -- \
  --headless --duration 30 --no-geoip
```

実行ごとに新しいセッションディレクトリが作られるため、以前のキャプチャは上書きされません。ディレクトリのモードは `0700`、証拠ファイルのモードは `0600` です。`--output-format` で形式を選びます。

| 形式 | ファイル |
| --- | --- |
| `jsonl` | 接続イベントを含む `connections.jsonl` |
| `pcapng` | 注釈付きパケットを含む `capture.pcapng` |
| `pcap` | `capture.pcap` と最終的な接続メタデータを含む `capture.pcap.connections.jsonl` |
| `both`（既定） | `connections.jsonl` と `capture.pcapng` |

`--json-log`、`--pcap-export`、`--pcapng-export` は `--output-dir` と併用できません。`--output-dir` を使わない場合、これらの RustNet オプションは一時的な Pod 内にファイルを書き込むため、終了前にコピーしてください。

## 権限と復旧

エクスポートには、Pod の作成、接続、削除に加えて `pods/exec` 権限が必要です。[RBAC の例](../deploy/rbac.yaml)を参照してください。

正常終了、タイムアウト、Ctrl+C、SIGTERM、または接続失敗時に、プラグインは RustNet に正常停止を要求し、書き込みが終わるまで最大 60 秒待ちます。コピー中は補助コンテナが共有 `emptyDir` を維持します。ラッパーは `CHOWN` を使ってファイルの所有権を移し、補助コンテナはすべての capability を破棄します。イメージは SIGINT/SIGTERM による正常停止に対応し、`/bin/sh`、`sleep`、`chown`、`touch`、`cat`、`mv`、`sha256sum` を含む必要があります。公式の Debian ベースイメージにはこれらのツールが含まれます。

プラグインは各ファイルをローカルに転送し、SHA-256 チェックサムを検証してから Pod を削除します。コピーのタイムアウトは 5 分で、キャプチャ時と同じ namespace、context、kubeconfig を使います。停止、コピー、検証のいずれかに失敗すると、Pod とローカルの `.partial` ファイルを残し、復旧用の `kubectl cp` コマンドを表示します。ファイルを復旧した後、Pod を手動で削除してください。`kubectl cp` には補助コンテナのイメージに `tar` が必要です。Pod の `emptyDir` はコンテナ終了後も残りますが、Pod の削除やノードの喪失後は利用できません。

JSONL イベントには、イベント発生時に利用できたメタデータが含まれます。停止時点で追跡中の接続について最終的なメタデータを補完するには、`pcap` とその JSONL サイドカーを選んでください。Kubernetes のスキャン間に終了したプロセスの Pod 情報を取得するには、[RustNet #634](https://github.com/domcyrus/rustnet/pull/634) を含むイメージが必要です。連携したエクスポート手順については[issue #20](https://github.com/domcyrus/kubectl-rustnet/issues/20)を参照してください。

## 統合テスト

エクスポートテストには、シグナルによる正常停止と headless モードに対応した RustNet イメージが必要です。短い通信のテストには Python イメージも必要です。両方のイメージをローカルクラスタに読み込み、独立した kubeconfig を使用してください。

```bash
KUBECONFIG=/path/to/test-kubeconfig \
KUBECTL_RUSTNET_BIN="$PWD/kubectl-rustnet" \
RUSTNET_EXPORT_IMAGE=rustnet:export-test \
RUSTNET_EXPORT_WORKLOAD_IMAGE=python:3-slim \
go test ./e2e -run '^TestExport' -v -timeout 300s
```

テストは正常終了、タイムアウト、SIGINT/SIGTERM、PCAP サイドカー、転送失敗後の復旧、Kubernetes Pod 内の短い TCP/UDP 通信を対象とします。デバッグ Pod の削除後もローカルの証拠ファイルが読み取れることを確認します。
