# ビルド

Go 1.24 以降。Linux からでも Windows 用にビルドできます。

```bash
cd src
go test ./core
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o ShotPanel.exe .
```

- `rsrc_windows_amd64.syso` はアイコンとマニフェスト（`build/icon.ico`・`build/app.manifest` から `rsrc -arch amd64 -ico build/icon.ico -manifest build/app.manifest -o rsrc_windows_amd64.syso` で作成）。
- `go.mod` で `golang.org/x/sys` を GitHub の `github.com/golang/sys` に置き換えている（golang.org に届かない環境用）。
- 画面は `ui/index.html`（exe に埋め込み）。
