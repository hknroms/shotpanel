#!/usr/bin/env python3
"""配布用の zip を作る。

  python3 dev/make_zip.py <ShotPanel フォルダ> <出力.zip>

ShotPanel フォルダには ShotPanel.exe・はじめにお読みください.txt・source.zip を入れておく。
Python の zipfile は日本語のファイル名に UTF-8 の印（flag 0x800）を付けるので、
Windows のエクスプローラーで解凍しても名前が化けない。
（Linux の zip コマンドで作ると印が付かず、Windows では Shift_JIS と見なされて化ける。v0.10.1・v0.10.2 の最初の zip で起きた）
"""
import os, sys, zipfile

src, out = sys.argv[1], sys.argv[2]
base = os.path.basename(os.path.normpath(src))
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
    for name in sorted(os.listdir(src)):
        z.write(os.path.join(src, name), f"{base}/{name}")
with zipfile.ZipFile(out) as z:
    for i in z.infolist():
        if any(ord(c) > 127 for c in i.filename) and not i.flag_bits & 0x800:
            sys.exit(f"UTF-8 の印が付いていない: {i.filename}")
        print(hex(i.flag_bits), i.filename)
