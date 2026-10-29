package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MameDefaultKeys はMAME本体の初期キー（MAME 0.289 の src/emu/inpttype.ipp より）。
// 2PのB6〜B10は初期状態でキーが無い。
func MameDefaultKeys() map[string]map[string]string {
	return map[string]map[string]string{
		"1": {"UP": "UP", "DOWN": "DOWN", "LEFT": "LEFT", "RIGHT": "RIGHT",
			"B1": "LCONTROL", "B2": "LALT", "B3": "SPACE", "B4": "LSHIFT", "B5": "Z", "B6": "X",
			"B7": "C", "B8": "V", "B9": "B", "B10": "N", "START": "1", "COIN": "5"},
		"2": {"UP": "R", "DOWN": "F", "LEFT": "D", "RIGHT": "G",
			"B1": "A", "B2": "S", "B3": "Q", "B4": "W", "B5": "E", "B6": "",
			"B7": "", "B8": "", "B9": "", "B10": "", "START": "2", "COIN": "6"},
		"0": {"SERVICE1": "9", "SERVICE": "F2", "TILT": "T",
			"MENU": "TAB", "PAUSE": "F5", "RESET": "F3", "FFWD": "INSERT",
			"QSAVE": "LSHIFT+F6", "QLOAD": "LSHIFT+F7", "EXIT": "ESC"},
	}
}

// Funcs は1P・2Pの機能の一覧。
var Funcs = []string{"UP", "DOWN", "LEFT", "RIGHT", "B1", "B2", "B3", "B4", "B5", "B6", "B7", "B8", "B9", "B10", "START", "COIN"}

// SysFuncs は共通の機能 → MAMEの入力名。
var SysFuncs = map[string]string{"SERVICE1": "SERVICE1", "SERVICE": "SERVICE", "TILT": "TILT",
	"MENU": "UI_MENU", "PAUSE": "UI_PAUSE", "RESET": "UI_SOFT_RESET", "FFWD": "UI_FAST_FORWARD",
	"QSAVE": "UI_SAVE_STATE_QUICK", "QLOAD": "UI_LOAD_STATE_QUICK", "EXIT": "UI_CANCEL"}

// writable はこのソフトが default.cfg に書き込む共通機能（MAMEの操作キーは書き換えない）。
var writableSys = map[string]bool{"SERVICE1": true, "SERVICE": true, "TILT": true}

// PortType は機能名 → MAMEの入力名（P1_BUTTON1 など）。
func PortType(pl, fn string) string {
	if pl == "0" {
		return SysFuncs[fn]
	}
	switch fn {
	case "UP", "DOWN", "LEFT", "RIGHT":
		return "P" + pl + "_JOYSTICK_" + fn
	case "START":
		return "START" + pl
	case "COIN":
		return "COIN" + pl
	}
	return "P" + pl + "_BUTTON" + fn[1:]
}

func portIndex() map[string][2]string {
	m := map[string][2]string{}
	for _, pl := range []string{"1", "2"} {
		for _, fn := range Funcs {
			m[PortType(pl, fn)] = [2]string{pl, fn}
		}
	}
	for fn, t := range SysFuncs {
		m[t] = [2]string{"0", fn}
	}
	return m
}

// seqKeys は MAMEの入力列（"KEYCODE_F6 KEYCODE_LSHIFT OR ..."）の最初の組み合わせを "LSHIFT+F6" の形にする。
func seqKeys(seq string) string {
	first := strings.SplitN(seq, " OR ", 2)[0]
	var mods, keys []string
	not := false
	for _, t := range strings.Fields(first) {
		if t == "NOT" {
			not = true
			continue
		}
		if strings.HasPrefix(t, "KEYCODE_") && !not {
			k := strings.TrimPrefix(t, "KEYCODE_")
			switch k {
			case "LSHIFT", "RSHIFT", "LCONTROL", "RCONTROL", "LALT", "RALT":
				if len(strings.Fields(first)) > 1 {
					mods = append(mods, k)
					continue
				}
			}
			keys = append(keys, k)
		}
		not = false
	}
	return strings.Join(append(mods, keys...), "+")
}

// CfgDir はMAMEの cfg フォルダ（mame.ini の cfg_directory、無ければ cfg）。
func CfgDir(mameDir string) string {
	d := "cfg"
	for _, f := range FindInis(mameDir) {
		if v, _ := ReadIni(f.Path); v != nil && strings.TrimSpace(v["cfg_directory"]) != "" {
			d = strings.Trim(strings.TrimSpace(v["cfg_directory"]), `"`)
			if i := strings.Index(d, ";"); i >= 0 {
				d = d[:i]
			}
			break
		}
	}
	if !filepath.IsAbs(d) {
		d = filepath.Join(mameDir, d)
	}
	return d
}

var (
	reDefaultSys = regexp.MustCompile(`(?s)<system\s+name="default"\s*>(.*?)</system>`)
	rePort       = regexp.MustCompile(`(?s)\s*<port\b([^>]*?)(?:/>|>(.*?)</port>)`)
	reType       = regexp.MustCompile(`\btype="([^"]+)"`)
	reStdSeq     = regexp.MustCompile(`(?s)<newseq\s+type="standard"\s*>(.*?)</newseq>`)
	reKeycode    = regexp.MustCompile(`KEYCODE_([A-Z0-9_]+)`)
)

// MameKeys はMAMEの default.cfg の内容を読む。
type MameKeys struct {
	Path      string                       `json:"path"`
	Exists    bool                         `json:"exists"`
	Keys      map[string]map[string]string `json:"keys"`      // 実際にMAMEが使うキー（"" = キー無し）
	Overrides []string                     `json:"overrides"` // ゲーム別にキーを変えているROM
	HasBackup bool                         `json:"hasBackup"`
}

// ReadMameKeys は default.cfg から1P・2Pのキーを読む（書かれていない所はMAMEの初期値）。
func ReadMameKeys(mameDir string) (*MameKeys, error) {
	dir := CfgDir(mameDir)
	p := filepath.Join(dir, "default.cfg")
	res := &MameKeys{Path: p, Keys: MameDefaultKeys()}
	if _, err := os.Stat(p + backupSuffix); err == nil {
		res.HasBackup = true
	}
	b, err := os.ReadFile(p)
	if err == nil {
		res.Exists = true
		idx := portIndex()
		if m := reDefaultSys.FindSubmatch(b); m != nil {
			for _, pm := range rePort.FindAllSubmatch(m[1], -1) {
				t := reType.FindSubmatch(pm[1])
				if t == nil {
					continue
				}
				pf, ok := idx[string(t[1])]
				if !ok {
					continue
				}
				seq := reStdSeq.FindSubmatch(pm[2])
				if seq == nil {
					continue
				}
				res.Keys[pf[0]][pf[1]] = seqKeys(string(seq[1]))
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// ゲーム別の cfg で1P・2Pのキーを変えているもの
	idx := portIndex()
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(n), ".cfg") || strings.EqualFold(n, "default.cfg") {
				continue
			}
			gb, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				continue
			}
			for _, pm := range rePort.FindAllSubmatch(gb, -1) {
				t := reType.FindSubmatch(pm[1])
				if t == nil {
					continue
				}
				if _, ok := idx[string(t[1])]; ok && reKeycode.Match(pm[2]) {
					res.Overrides = append(res.Overrides, strings.TrimSuffix(n, filepath.Ext(n)))
					break
				}
			}
		}
	}
	sort.Strings(res.Overrides)
	return res, nil
}

// WriteMameKeys は default.cfg に1P・2Pのキーを書く。MAMEの初期値と同じものは書かずに初期値に任せる。
func WriteMameKeys(mameDir string, keys map[string]map[string]string) (string, error) {
	dir := CfgDir(mameDir)
	p := filepath.Join(dir, "default.cfg")
	def := MameDefaultKeys()
	idx := portIndex()

	var ports strings.Builder
	type item struct{ pl, fn string }
	var items []item
	for _, pl := range []string{"1", "2"} {
		for _, fn := range Funcs {
			items = append(items, item{pl, fn})
		}
	}
	for _, fn := range []string{"SERVICE1", "SERVICE", "TILT"} {
		items = append(items, item{"0", fn})
	}
	for _, it := range items {
		pl, fn := it.pl, it.fn
		{
			k := keys[pl][fn]
			if k == def[pl][fn] {
				continue
			}
			v := "NONE"
			if k != "" {
				var parts []string
				for _, x := range strings.Split(k, "+") {
					if !regexp.MustCompile(`^[A-Z0-9_]+$`).MatchString(x) {
						return "", fmt.Errorf("キー名が不正です: %s", k)
					}
					parts = append(parts, "KEYCODE_"+x)
				}
				v = strings.Join(parts, " ")
			}
			fmt.Fprintf(&ports, "\n            <port type=\"%s\">\n                <newseq type=\"standard\">%s</newseq>\n            </port>", PortType(pl, fn), v)
		}
	}

	raw, err := os.ReadFile(p)
	var out string
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		out = "<?xml version=\"1.0\"?>\n<!-- This file is autogenerated; comments and unknown tags will be stripped -->\n<mameconfig version=\"10\">\n    <system name=\"default\">\n        <input>" +
			ports.String() + "\n        </input>\n    </system>\n</mameconfig>\n"
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
		if _, e := os.Stat(p + backupSuffix); os.IsNotExist(e) {
			os.WriteFile(p+backupSuffix, nil, 0644) // 空 = 元はファイルが無かった
		}
	} else {
		if _, e := os.Stat(p + backupSuffix); os.IsNotExist(e) {
			if err := os.WriteFile(p+backupSuffix, raw, 0644); err != nil {
				return "", fmt.Errorf("バックアップを作れませんでした: %w", err)
			}
		}
		s := string(raw)
		loc := reDefaultSys.FindStringSubmatchIndex(s)
		if loc == nil {
			// <system name="default"> が無ければ </mameconfig> の前に足す
			i := strings.LastIndex(s, "</mameconfig>")
			if i < 0 {
				return "", fmt.Errorf("%s の形式が読めません", p)
			}
			s = s[:i] + "    <system name=\"default\">\n        <input>" + ports.String() + "\n        </input>\n    </system>\n" + s[i:]
		} else {
			body := s[loc[2]:loc[3]]
			// 1P・2Pのキーの行を取り除く
			body = rePort.ReplaceAllStringFunc(body, func(m string) string {
				if t := reType.FindStringSubmatch(m); t != nil {
					if pf, ok := idx[t[1]]; ok && (pf[0] != "0" || writableSys[pf[1]]) {
						return ""
					}
				}
				return m
			})
			if i := strings.Index(body, "</input>"); i >= 0 {
				head := strings.TrimRight(body[:i], " \t\r\n")
				body = head + ports.String() + "\n        " + body[i:]
			} else if strings.Contains(body, "<input/>") || strings.Contains(body, "<input />") {
				body = regexp.MustCompile(`<input\s*/>`).ReplaceAllString(body, "<input>"+ports.String()+"\n        </input>")
			} else {
				body = strings.TrimRight(body, " \t\r\n") + "\n        <input>" + ports.String() + "\n        </input>\n    "
			}
			s = s[:loc[2]] + body + s[loc[3]:]
		}
		out = s
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}

// RestoreMameKeys は default.cfg を書き換え前に戻す（元が無かった場合は作ったファイルを消す）。
func RestoreMameKeys(mameDir string) error {
	p := filepath.Join(CfgDir(mameDir), "default.cfg")
	if b, err := os.ReadFile(p + backupSuffix); err == nil {
		if len(b) == 0 {
			os.Remove(p)
			return os.Remove(p + backupSuffix)
		}
		if err := os.WriteFile(p, b, 0644); err != nil {
			return err
		}
		return os.Remove(p + backupSuffix)
	}
	return fmt.Errorf("バックアップが見つかりません")
}

// UIKeys はMAMEの操作に使われているキー（ゲームの入力に使うと困るもの）。
var UIKeys = map[string]string{"ESC": "終了", "TAB": "メニュー", "F5": "一時停止", "TILDE": "画面表示", "F1": "", "F2": "", "F3": "リセット", "F4": "", "F6": "", "F7": "", "F8": "", "F9": "", "F10": "", "F11": "", "F12": "", "INSERT": "早送り", "SCRLOCK": ""}
