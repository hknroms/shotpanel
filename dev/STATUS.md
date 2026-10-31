# 引き継ぎ（STATUS） — 連射パネル設定（ShotPanel）

最終更新：2026-10-31（v0.10.2 公開）

新しい会話では、このリポジトリ（hknroms/shotpanel）をつないで、まず CLAUDE.md とこのファイルを読む。

---

## 1. いまの状態

- **最新版：v0.10.2（2026-10-31）** — 配布ページ https://hknroms.github.io/shotpanel/ （英語 /en/）に公開済み。
- **ユーザーの実機で「動いた」と確認できた最初の版**。MAMEUI 0.289（mameui.exe）＋ アケコン／パッドで、レバー・ボタン・連射が効いた。
- **v0.10.1（2026-10-30 公開）**：①上の帯を2段・高さ固定（1段目＝ゲームと状態、2段目＝送っているキー。毎回作り直さず文字だけ書き換える）。v0.10.0 でも入力が多いと微妙に動くとの報告（Windows でのフォントの違いで1px前後ずれると推測。Linux の Chromium では再現せず）。②新しくつないだコントローラーの配置の初期値を常に 8bs（右端にスタート）に（2026-10-30 ユーザー決定。以前は XInput・パッドだと "pad"）。
- 2026-10-30：作業用アカウントを shotpanel の共同編集者に追加（それまでは Claude の会話から push できなかった）。
- **v0.10.2（2026-10-31 公開）**：揺れの本当の原因が分かった。v0.10.1 を起動しても、常駐していた古い版（v0.9.x。「MAMEに送信中：キー」の1つの枠が折り返して画面全体が上下に動く）の画面が出ていただけだった（1つしか起動しない仕組みで、2つ目は先の方に「画面を出して」と送って終わる）。直したこと：①別の場所の exe が常駐していたら WMQuit（wmApp+3）を送り、2秒で終わらなければ強制終了して入れ替わる。新しい側に shotpanel.json が無ければ古い側から写す。②状況（送信中・送っているキー）をゲーム名の帯の下の独立した箱（#sendbox、高さ36px固定）に（ユーザー希望）。③ウィンドウのタイトルと下の帯に版（appVersion）を表示。
- 使っているのはユーザーと友人だけ。配布ページはログインなしで誰でも見られる（ログインの仕組みは残してある。後述）。

## 2. このソフトは何か

- Windows 10／11 用の常駐ソフト。パッド・アケコンの入力を読み取り、連射・ボタン入れ替え・レバーの方向の付け替えをしてから、**キーボードの入力（SendInput のスキャンコード）として MAMEUI に送る**。
- **MAMEUI 専用**（2026-10-30 にユーザーが決定）。ユーザーの MAMEUI は `D:\Documents\My Game\mame\mameui64-0-289\mameui.exe`（0.289）。そのフォルダに mame.exe は無い。
  - 当初は 0.289 とサンダーメーム（古いMAME）両対応を狙っていたが、MAMEUI 専用にした。古いMAME向けのコード（ゲーム別 ini の SetGameDirect など）は一部残っているが、画面からは使っていない。
- 2P まで。シューティング・アクション前提。

## 3. MAMEUI について分かったこと（大事）

MAMEUI のソース（github.com/Robbbert/mameui の `src/osd/winui/`）で確かめた事実：

- MAMEUI の設定名（CONFIGNAME）は **`mame`**。ゲームを始めるとき（winui.cpp の RunMAME）は普通の MAME と同じく `mame_options::parse_standard_inis` で ini を読む。
- 読む ini は **`mameui.exe と同じフォルダの mame.ini`**（最初は無いこともある）→ 種類別（ini\arcade.ini・horizont.ini・vertical.ini・raster.ini・vector.ini・ini\source\*.ini）→ ゲーム別（ini\<ROM名>.ini）。
- MAMEUI は「デフォルトのゲームオプション」を閉じたときなどに `global_save_ini()` で **exe と同じフォルダの mame.ini と ini\mame.ini の両方**を書き出す。MAMEUI を開いたまま ini を書き換えても、MAMEUI が閉じるときに上書きされて消える。
- **`MAMEUI.ini`（exe と同じフォルダ）は MAMEUI の画面の設定**で、MAME のオプションではない。v0.9.x まではこれを mame.ini と取り違え、keyboardprovider が無い＝古いMAME＝OK と判断して ✓ を付けていた。これが「何をしても効かない」の本当の原因だった（v0.10.0 で直した）。
- MAMEUI のウィンドウタイトル：`Raiden [raiden] - MAMEUI 0.289 (LLP64)`（appname が MAMEUI）。
- keyboardprovider が auto（＝rawinput）のままだと、ソフトから送ったキー（SendInput）を MAME は受け取らない。dinput か win32 にする必要がある。ユーザーの環境は今 **win32** 設定で動いている。
- MAME の「入力設定」で「デフォルトに戻す」をすると、キーボード＋パッド両方の初期割り当てに戻り、MAME がパッドも直接読んで二重に動く。このソフトは cfg\default.cfg に1P・2Pの全部の入力を「キーボードだけ」と書いてこれを防ぐ（joystick は 1 のまま＝アナログのゲームはそのまま遊べる）。

## 4. 構成（src/）

| 場所 | 中身 |
|---|---|
| `main.go` | 本体。WebView2 の画面、1ms ごとのループ（入力→エンジン→キー送信）、MAME の検出、画面からの API（`api(cmd, arg)`） |
| `core/` | OS に依存しない部分。**`go test ./core` が Linux で通る** |
| `core/config.go` | 設定ファイル `shotpanel.json`（exe と同じフォルダ）の型。DevSettings（コントローラー：1P/2P/無効・配置・位置→物理入力の対応）、GameProfile（ゲームごと。`_common` が共通設定）、Keys（"1"/"2"/"0"→機能→キー名） |
| `core/engine.go` | 連射（フレーム数パターン。1F=1/60秒）・同時押し・相手側への出力・レバーの方向ごとの割り当て（cfg の `dir_up` など）・交互連打 |
| `core/mameini.go` | **MAMEUI の ini**（FindInis・MameUISetup・MameReady・RestoreMame）、ウィンドウタイトルの解析、`mame -listxml` の解析 |
| `core/mamekeys.go` | **cfg\default.cfg** の読み書き（1P・2P・サービス等を「キーボードだけ」に）、MAME 0.289 の初期キー、ゲーム別 cfg の検出 |
| `core/devdefaults.go` | コントローラーの種類ごとの初期のボタン対応（XInput／PS系HID） |
| `win/` | Windows API：Raw Input（HID）・XInput の読み取り、SendInput、MAME の窓の検出、通知領域、フォルダを選ぶ画面（COM の IFileOpenDialog）、自動起動 |
| `ui/index.html` | 画面（exe に埋め込み）。素の HTML/JS |
| `third_party/go-webview2/` | jchv/go-webview2 の写し。**IsDialogMessage を外してある**（入力欄に文字が打てなかったため）。go.mod の replace で使用 |
| `rsrc_windows_amd64.syso` | アイコン（リソースID 2）とマニフェスト（ID 1）。`build/icon.ico`・`build/app.manifest` から rsrc で作成 |

## 5. ビルドと公開

```bash
cd src
export GOPROXY=direct GOSUMDB=off GOFLAGS=-mod=mod   # proxy.golang.org・golang.org に届かない環境用（x/sys は go.mod で github.com/golang/sys に置き換え済み）
go test ./core
GOOS=windows GOARCH=amd64 go vet . ./win
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o ShotPanel.exe .
```

- 実機（Windows・コントローラー・MAMEUI）での確認はこちらではできない。画面は Playwright（Chromium）で `ui/index.html` を開き、`window.api` を仮のデータに差し替えて確かめてきた。
- 公開の手順：zip は **`python3 dev/make_zip.py <ShotPanelフォルダ> <出力.zip>` で作る**（Linux の zip コマンドだと日本語のファイル名に UTF-8 の印が付かず、Windows で化ける）。zip（`ShotPanel/ShotPanel.exe`・`ShotPanel/はじめにお読みください.txt`・`ShotPanel/source.zip`）を `docs/files/ShotPanel_v<版>.zip` に置く → `docs/versions.json` の先頭に版を足す（日本語と `en` の英語の両方）→ 使い方が変わったら `docs/index.html` と `docs/en/index.html` も直す。
- 「はじめにお読みください.txt」は UTF-8（BOM付き）・CRLF。最新の zip から取り出して書き足していく。
- **配布ページ（docs/）への公開の前はユーザーに確認する**（CLAUDE.md）。src/ や dev/ は確認なしで commit・push してよい。
- docs/index.html の上部に `window.REQUIRE_LOGIN = false;` がある。true にすると gradius3 と同じ Google ログイン＋承認制（Firebase プロジェクト gradius3-login）になる。

## 6. 画面の仕様（ユーザーと決めたこと）

- 返事・画面とも日本語。英語の配布ページあり（ソフト本体の画面は日本語のみ）。
- ボタンの呼び方は **MAME に合わせて「ボタン1〜6」**（ショット・キックの呼び方は使わない）。
- 一番上に「はじめにやること」（フォルダを選ぶ → MAMEUI をこのソフト用にする → コントローラー → ゲームで試す）。
- つないだコントローラーを一覧し、それぞれ 1P／2P／無効。選んだコントローラーの真下にイラスト（アケコン：レバーはボタンの左、配置は 8ボタン／8ボタン＋右端スタート／6／3／ゲームパッド）。↑↓ で並び順を入れ替え。
- 配置パターンは普段閉じる。番号のみ（名前は付けない）：2B-1〜3（2ボタンゲーム）、3B-1・3B-2（3ボタンゲーム。3B-3 は隠し）、4B-1〜7（ネオジオ。横一列・D落ち・A落ち・ボックスAC/BD・ボックスAB/CD・A上段・SC19）、6B-1。
- ボタンの個別設定：動作（単発・連射・なし）／割り当て（2つ以上選ぶと同時押し）／「ほかの割り当て」（ボタン7〜10・サービス・テスト・ティルト・MAMEの操作。起動中のゲームの情報で絞る）／連射（30・20・15・12・10・8.6・7.5・5・3 と、■□のマスで押す・離すフレーム数を自由に）／拡張設定に「出力先（相手側のプレイヤー）」／位置の入れ替え・押して登録し直す。
- レバーを押すと方向ごとの割り当て（通常・連射・なし、行き先、上下反対・左右反対）。拡張機能に交互連打と「左スティックもレバー」。
- 「いまの割り当て」は普段閉じる。手連射測定は画面の一番下のリンクから（測るボタンを選べる）。
- 画面上はゲーム名の帯（高さ40px固定）と、その下の独立した状況の箱（状態の枠＝送信中／待機中／一時停止中・幅固定、送っているキー＝無いときは「なし」。高さ36px固定）。v0.10.2〜。

## 7. まだやっていないこと・候補

- **MAME4droid（Android）版**：ユーザー希望。Android ではほかのアプリにキーを送れないので、MAME の Lua プラグイン（MAME4droid は 0.289 ベース）で連射・割り当てをやる案。プラグインの読み込みができるか、ユーザーにバージョンと plugins フォルダの有無を確かめてもらう段階で止まっている。同じプラグインは PC の MAMEUI でも使える可能性がある（キー送信が要らず、フレームに完全同期）。
- 2レバー（右スティックを2本目のレバーに）：後から足す候補。
- トラックボール・ダイヤル・ハンドルは扱わない（MAME が直接読む）。
- ソフト本体の英語化：未対応（提案済み・返事なし）。
- アケコンの機種名から配置を自動で選ぶ（ユーザーの実物は右端にスタート。機種名は未確認）。
- 「MAMEUIのフォルダ」は起動中の MAMEUI からも取れる（このフォルダを使う）。

## 8. 経緯メモ（同じ失敗をしないために）

- v0.10.1：揺れを直したつもりが「全然変わってない」と言われた。実は古い版が常駐したままで、新しい exe を起動しても古い版の画面が出ていた。**直したのに変わらないと言われたら、まず画面に出ている版（v0.10.2〜はタイトルと画面の下に出る）を確かめてもらう。**

- v0.1〜v0.9.4：mame.ini の場所を MAME の一般的な置き場所で決め打ちしていた。MAMEUI の実際の読み方を確かめずに、ユーザーに何度も確認をお願いしてしまった。**MAMEUI まわりは推測せず、MAMEUI のソースで確かめること。**
- v0.9.1：新しい MAME のタイトルの形（`… [rom] - MAME 0.289 …`）に対応していなかったので、起動中のゲームを見つけられなかった。
- v0.9.2：設定が済んで「残り」の一覧が空のとき、JSON の null を「まだ読めていない」と取り違えていた（Go 側は空の配列で返すようにした）。
- v0.9.4：MAMEUI が開いている間は書き換えを止めるようにした（`win.MameAppOpen`）。
- 2P のボタン6は MAME の初期設定でキーが無い（v0.1 の I・K は誤り）。今は 2P ボタン5=E、ボタン6=H。
