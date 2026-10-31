# プラグイン（src/core/mameplugin/init.lua）の試験

MAME の代わりに manager / emu を差し替えて、Linux の Lua 5.4 で init.lua を動かす。
通知の登録が捨てられると外れる（MAME と同じ）ところも再現している（v0.11.1 の不具合はこれで見つかる）。

```bash
# Lua 5.4 の小さなホスト（liblua5.4 があれば作れる）
gcc dev/plugintest/luahost.c -o /tmp/lua /usr/lib/x86_64-linux-gnu/liblua5.4.so.0
# 試験用のフォルダ（harness.lua は /tmp/pt を使う）
mkdir -p /tmp/pt/shotpanel && cp src/core/mameplugin/init.lua /tmp/pt/shotpanel/
# s1〜s3.txt（state.txt の見本。core の EncodeState で作ったもの）も /tmp/pt に写す：cp dev/plugintest/s?.txt /tmp/pt/
/tmp/lua dev/plugintest/harness.lua   # 最後に OK と出れば成功
```
