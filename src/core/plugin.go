package core

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MAME の Lua プラグイン（MAMEUI の plugins\shotpanel に入れる）。
// boot.lua は MAME 0.289 のもの（plugins フォルダに無いときだけ入れる。無いとプラグインが1つも動かない）。
//
//go:embed mameplugin/init.lua mameplugin/plugin.json
var pluginFS embed.FS

//go:embed mameplugin/boot.lua
var bootLua []byte

const PluginName = "shotpanel"

// StateSize は state.txt の大きさ（毎回この大きさで先頭から上書きする）。
const StateSize = 4096

// PluginsDir は MAMEUI の plugins フォルダ（mame.ini の pluginspath の最初の場所。無ければ plugins）。
func PluginsDir(dir string) string {
	d := "plugins"
	if v, err := ReadIni(filepath.Join(dir, "mame.ini")); err == nil {
		for _, x := range strings.Split(strings.Trim(strings.TrimSpace(v["pluginspath"]), `"`), ";") {
			if x = strings.TrimSpace(x); x != "" {
				d = x
				break
			}
		}
	}
	if !filepath.IsAbs(d) {
		d = filepath.Join(dir, d)
	}
	return d
}

// PluginDir はこのソフトのプラグインの置き場所。
func PluginDir(dir string) string { return filepath.Join(PluginsDir(dir), PluginName) }

// StatePath はプラグインに渡す state.txt の場所。
func StatePath(dir string) string { return filepath.Join(PluginDir(dir), "state.txt") }

// PluginInfo はプラグインの入り具合。
type PluginInfo struct {
	Dir       string   `json:"dir"`
	Installed bool     `json:"installed"` // ファイルが入っていて、中身が最新
	Enabled   bool     `json:"enabled"`   // plugin.ini・mame.ini で有効
	Boot      bool     `json:"boot"`      // plugins\boot.lua がある
	Ready     bool     `json:"ready"`
	Running   string   `json:"running"` // プラグインが動いているゲームの ROM 名（running.txt）
	Reasons   []string `json:"reasons"`
}

func pluginFiles() map[string][]byte {
	m := map[string][]byte{}
	for _, n := range []string{"init.lua", "plugin.json"} {
		b, _ := pluginFS.ReadFile("mameplugin/" + n)
		m[n] = b
	}
	return m
}

// GetPluginInfo はプラグインが入って有効になっているかを調べる。
func GetPluginInfo(dir string) PluginInfo {
	p := PluginInfo{Dir: PluginDir(dir), Installed: true}
	for n, want := range pluginFiles() {
		if b, err := os.ReadFile(filepath.Join(p.Dir, n)); err != nil || !bytes.Equal(normNL(b), normNL(want)) {
			p.Installed = false
		}
	}
	if !p.Installed {
		p.Reasons = append(p.Reasons, "プラグインのファイルがまだ入っていない（または古い）："+p.Dir)
	}
	_, err := os.Stat(filepath.Join(PluginsDir(dir), "boot.lua"))
	p.Boot = err == nil
	if !p.Boot {
		p.Reasons = append(p.Reasons, "plugins フォルダに boot.lua が無い（このソフトが入れます）")
	}
	p.Enabled = true
	if v, _ := ReadIni(filepath.Join(dir, "mame.ini")); v != nil {
		if strings.TrimSpace(v["plugins"]) == "0" {
			p.Enabled = false
			p.Reasons = append(p.Reasons, "mame.ini で plugins が 0（プラグインを使わない）になっている")
		}
		if hasName(v["noplugin"], PluginName) {
			p.Enabled = false
			p.Reasons = append(p.Reasons, "mame.ini の noplugin に shotpanel がある")
		}
	}
	if v, _ := ReadIni(filepath.Join(IniDir(dir), "plugin.ini")); v != nil {
		if s, ok := v[PluginName]; ok && strings.TrimSpace(s) != "1" {
			p.Enabled = false
			p.Reasons = append(p.Reasons, "plugin.ini で shotpanel が 0（無効）になっている")
		}
	}
	if b, err := os.ReadFile(filepath.Join(p.Dir, "running.txt")); err == nil {
		p.Running = strings.TrimSpace(string(b))
	}
	p.Ready = p.Installed && p.Enabled && p.Boot
	return p
}

func hasName(list, name string) bool {
	for _, x := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(x), name) {
			return true
		}
	}
	return false
}

func normNL(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// InstallPlugin はプラグインを MAMEUI に入れて有効にする（MAMEUI は閉じておく）。
func InstallPlugin(dir string) ([]string, error) {
	var done []string
	pd := PluginDir(dir)
	if err := os.MkdirAll(pd, 0755); err != nil {
		return nil, fmt.Errorf("%s を作れませんでした: %w", pd, err)
	}
	for n, b := range pluginFiles() {
		if err := os.WriteFile(filepath.Join(pd, n), b, 0644); err != nil {
			return done, fmt.Errorf("%s を書き込めませんでした: %w", n, err)
		}
	}
	done = append(done, pd)
	if f := StatePath(dir); !fileExists(f) {
		os.WriteFile(f, EncodeState(0, nil), 0644)
	}
	boot := filepath.Join(PluginsDir(dir), "boot.lua")
	if !fileExists(boot) {
		if err := os.WriteFile(boot, bootLua, 0644); err != nil {
			return done, fmt.Errorf("boot.lua を書き込めませんでした: %w", err)
		}
		os.WriteFile(filepath.Join(pd, "boot.lua.installed"), nil, 0644) // 元に戻すとき消す印
		done = append(done, boot)
	}
	// plugin.ini（MAMEUI の ini フォルダ）で有効に
	pini := filepath.Join(IniDir(dir), "plugin.ini")
	if !fileExists(pini) && !fileExists(pini+backupSuffix) {
		os.MkdirAll(filepath.Dir(pini), 0755)
		os.WriteFile(pini+backupSuffix, nil, 0644) // 空 = 元はファイルが無かった
	}
	if ch, err := SetIni(pini, map[string]string{PluginName: "1"}, nil, true); err != nil {
		return done, fmt.Errorf("plugin.ini を書き換えられませんでした: %w", err)
	} else if len(ch) > 0 {
		done = append(done, pini)
	}
	// mame.ini で plugins 0 にしていたら 1 に
	main := filepath.Join(dir, "mame.ini")
	if v, _ := ReadIni(main); v != nil && strings.TrimSpace(v["plugins"]) == "0" {
		if _, err := SetIni(main, map[string]string{"plugins": "1"}, nil, true); err != nil {
			return done, err
		}
		done = append(done, main)
	}
	return done, nil
}

// UninstallPlugin はプラグインを外す。
func UninstallPlugin(dir string) error {
	pd := PluginDir(dir)
	if fileExists(filepath.Join(pd, "boot.lua.installed")) {
		os.Remove(filepath.Join(PluginsDir(dir), "boot.lua"))
	}
	pini := filepath.Join(IniDir(dir), "plugin.ini")
	if fileExists(pini + backupSuffix) {
		if err := RestoreMame(pini); err != nil {
			return err
		}
	} else {
		SetIni(pini, map[string]string{PluginName: "0"}, map[string]bool{PluginName: true}, false)
	}
	return os.RemoveAll(pd)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// PluginReq はプラグインに渡す1つの入力（行き先は MAME の入力名）。
type PluginReq struct {
	N       uint64
	On, Off int
	A, B    []string
}

// EncodeState は state.txt の中身を作る（StateSize バイト。足りない分は空白）。
func EncodeState(seq uint64, reqs []PluginReq) []byte {
	var b strings.Builder
	s := strconv.FormatUint(seq, 10)
	b.WriteString("SHOTPANEL 1 " + s + "\n")
	tail := "E " + s + "\n"
	for _, r := range reqs {
		if len(r.A) == 0 {
			continue
		}
		bb := "-"
		if len(r.B) > 0 {
			bb = strings.Join(r.B, ",")
		}
		line := fmt.Sprintf("R %d %d %d %s %s\n", r.N, r.On, r.Off, strings.Join(r.A, ","), bb)
		if b.Len()+len(line)+len(tail) > StateSize {
			break
		}
		b.WriteString(line)
	}
	b.WriteString(tail)
	out := []byte(b.String())
	return append(out, bytes.Repeat([]byte(" "), StateSize-len(out))...)
}

// uiOnly は MAME の入力（ポート）ではない、MAME の操作。プラグインでは押せないのでキーで送る。
var uiOnly = map[string]bool{"MENU": true, "PAUSE": true, "RESET": true, "FFWD": true, "QSAVE": true, "QLOAD": true, "EXIT": true}

// SplitForPlugin はエンジンの出力を、プラグインで押すもの（MAME の入力名）と、キーで送るもの（MAMEの操作）に分ける。
func SplitForPlugin(reqs []Req) (plug []PluginReq, keys []Req) {
	conv := func(ts []string) (p, k []string) {
		for _, t := range ts {
			pl, fn, _ := strings.Cut(t, ":")
			if strings.HasPrefix(fn, "SYS:") {
				pl, fn = "0", fn[4:]
			}
			if pl == "0" && uiOnly[fn] {
				k = append(k, t)
				continue
			}
			if tok := PortType(pl, fn); tok != "" {
				p = append(p, tok)
			}
		}
		return
	}
	for _, r := range reqs {
		pa, ka := conv(r.A)
		pb, kb := conv(r.B)
		if len(pa) > 0 || len(pb) > 0 {
			plug = append(plug, PluginReq{N: r.N, On: r.On, Off: r.Off, A: pa, B: pb})
		}
		if len(ka) > 0 || len(kb) > 0 {
			k := r
			k.A, k.B = ka, kb
			keys = append(keys, k)
		}
	}
	sort.Slice(plug, func(i, j int) bool { return plug[i].N < plug[j].N })
	return
}

// PlugLabel は画面に出す表示（"P1_BUTTON1 連1/1" など）。
func PlugLabel(r PluginReq) string {
	s := strings.Join(r.A, "+")
	if len(r.B) > 0 {
		s += "⇔" + strings.Join(r.B, "+")
	}
	if r.Off > 0 {
		s += fmt.Sprintf("(%d/%d)", r.On, r.Off)
	}
	return s
}

// Ack はプラグインからの返事（ack.txt：「ROM名 受け取ったseq フレーム数 画面の回数」）。
type Ack struct {
	Rom    string
	Seq    uint64
	Frames uint64
	Hz     float64
}

// ParseAck は ack.txt を読む。
func ParseAck(b []byte) (Ack, bool) {
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return Ack{}, false
	}
	a := Ack{Rom: f[0]}
	var err error
	if a.Seq, err = strconv.ParseUint(f[1], 10, 64); err != nil {
		return Ack{}, false
	}
	if a.Frames, err = strconv.ParseUint(f[2], 10, 64); err != nil {
		return Ack{}, false
	}
	if len(f) >= 4 {
		a.Hz, _ = strconv.ParseFloat(f[3], 64)
	}
	return a, true
}
