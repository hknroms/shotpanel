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

// plugLink はプラグインとのつながり（画面のチュートリアルで使う）。
type plugLink struct {
	Loaded bool    `json:"loaded"` // 起動中のゲームでプラグインが読み込まれた（running.txt）
	Linked bool    `json:"linked"` // プラグインが ShotPanel の state.txt を受け取っている（ack.txt）
	Fps    float64 `json:"fps"`    // 1秒あたりのフレーム数（実際の速さ）
	Hz     float64 `json:"hz"`     // ゲームの画面の回数（100% のときの速さ）
}

// ackBase は1秒あたりのフレーム数を測るための、前に見た ack.txt。
type ackBase struct {
	rom    string
	frames uint64
	t      time.Time // ack.txt を書いた時刻（ファイルの更新時刻）
	fps    float64
}

// readAck は ack.txt を読んで、つながりと速さを調べる（detectMame から 0.25秒ごと）。
func (a *App) readAck(pd, rom string) plugLink {
	p := filepath.Join(pd, "ack.txt")
	st, err := os.Stat(p)
	if err != nil {
		a.ackBase = ackBase{}
		return plugLink{}
	}
	b, err := os.ReadFile(p)
	ack, ok := core.ParseAck(b)
	if err != nil || !ok || ack.Rom != rom {
		return plugLink{}
	}
	mt := st.ModTime()
	ab := &a.ackBase
	if ab.rom != rom || ack.Frames < ab.frames || ab.t.IsZero() {
		*ab = ackBase{rom: rom, frames: ack.Frames, t: mt}
	} else if d := mt.Sub(ab.t); d >= 2*time.Second {
		ab.fps = float64(ack.Frames-ab.frames) / d.Seconds()
		ab.frames, ab.t = ack.Frames, mt
	}
	a.st.mu.Lock()
	wseq := a.st.seq
	a.st.mu.Unlock()
	fresh := time.Since(mt) < 3*time.Second
	if !fresh {
		ab.fps = 0
	}
	return plugLink{
		Linked: fresh && ack.Seq > 0 && ack.Seq <= wseq && wseq-ack.Seq <= 40,
		Fps:    ab.fps,
		Hz:     ack.Hz,
	}
}
