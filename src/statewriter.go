//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"time"

	"shotpanel/core"
)

// stateWriter は MAME のプラグインに渡す state.txt を書く。
// ファイルは開いたままにして、毎回先頭から同じ大きさで上書きする（プラグインは毎フレーム先頭から読む）。
// 変わったときと、0.25秒ごと（生きている印）に書く。
type stateWriter struct {
	mu     sync.Mutex
	dir    string // MAMEUI のフォルダ
	path   string
	pathAt time.Time
	f      *os.File
	seq    uint64
	last   []byte // 前に書いた中身（seq を除く）
	lastAt time.Time
	failAt time.Time
}

func (w *stateWriter) write(now time.Time, mameDir string, reqs []core.PluginReq) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if mameDir != w.dir || now.Sub(w.pathAt) > 3*time.Second {
		p := core.StatePath(mameDir)
		if p != w.path {
			w.closeLocked()
			w.path = p
		}
		w.dir, w.pathAt = mameDir, now
	}
	if w.f == nil {
		if now.Sub(w.failAt) < time.Second {
			return
		}
		if _, err := os.Stat(filepath.Dir(w.path)); err != nil {
			w.failAt = now // プラグインがまだ入っていない
			return
		}
		f, err := os.OpenFile(w.path, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			w.failAt = now
			return
		}
		w.f, w.last = f, nil
	}
	body := core.EncodeState(0, reqs)
	if bytes.Equal(body, w.last) && now.Sub(w.lastAt) < 250*time.Millisecond {
		return
	}
	w.seq++
	if _, err := w.f.WriteAt(core.EncodeState(w.seq, reqs), 0); err != nil {
		w.closeLocked()
		w.failAt = now
		return
	}
	w.last, w.lastAt = body, now
}

// close は何も押していない状態を書いて閉じる。
func (w *stateWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closeLocked()
}

func (w *stateWriter) closeLocked() {
	if w.f == nil {
		return
	}
	w.seq++
	w.f.WriteAt(core.EncodeState(w.seq, nil), 0)
	w.f.Close()
	w.f, w.last = nil, nil
}

// plugDir はプラグインのフォルダ（state.txt と同じ場所。mame.ini を毎回読まないよう覚えておく）。
func (a *App) plugDir(mameDir string) string {
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	if a.st.dir == mameDir && a.st.path != "" {
		return filepath.Dir(a.st.path)
	}
	return core.PluginDir(mameDir)
}
