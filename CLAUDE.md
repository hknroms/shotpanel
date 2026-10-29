# CLAUDE.md — このリポジトリで作業する Claude への決まり

MAME用の連射・ボタン割り当てソフト「連射パネル設定（ShotPanel）」のリポジトリです。管理者は hknroms。

- 返事は日本語で短く。ユーザーは音声入力をよく使うので聞き間違いに注意（例:「メーム」「メム」「豆」=MAME、「手連車」=手連射、「グラスリー」=gradius3）。
- **配布ページ（docs/index.html・docs/versions.json・docs/files/）に新しい版を公開する push の前は、ユーザーに確認する。** 公開したら https://hknroms.github.io/shotpanel/ を伝える。
- 公開のページ（docs/ 以下）と、そこからたどれる所に、作業用アカウントの名前と Claude の名前を出さない。開発用の記録は dev/ に置く。
- コミットの名前は `git -c user.name=hknroms-claude -c user.email=noreply@hknroms.invalid commit …`。
- 新しい版を出すとき：`src/` でビルド → zip（ShotPanel.exe・はじめにお読みください.txt・source.zip）を `docs/files/ShotPanel_v<版>.zip` に置く → `docs/versions.json` の先頭に版を足す。
- 配布ページは gradius3 と同じ Google ログインの扉（Firebase プロジェクト gradius3-login）を使っている。gradius3 で承認された人がそのまま使える。
