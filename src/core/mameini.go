package core

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// IniFile は見つかった mame.ini の情報。
type IniFile struct {
	Path             string `json:"path"`
	KeyboardProvider string `json:"keyboardprovider"` // 項目が無ければ ""（古いMAME）
	HasKbdProvider   bool   `json:"hasKbdProvider"`
	Joystick         string `json:"joystick"`
	HasBackup        bool   `json:"hasBackup"`
	IsReady          bool   `json:"ready"`
}

const backupSuffix = ".shotpanel-backup"

// FindInis はMAMEフォルダの中の設定ファイル候補を探す。
func FindInis(dir string) []IniFile {
	var out []IniFile
	seen := map[string]bool{}
	cands := []string{"mame.ini", "mame64.ini", "mameui.ini", "mameui64.ini", filepath.Join("ini", "mame.ini"), filepath.Join("ini", "mameui.ini")}
	// exe と同じ名前の ini（ThunderMAME など）
	if ents, err := os.ReadDir(dir); err == nil {
		for _, e := range ents {
			n := strings.ToLower(e.Name())
			if strings.HasSuffix(n, ".exe") && strings.Contains(n, "mame") {
				cands = append(cands, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))+".ini")
			}
		}
	}
	for _, c := range cands {
		p := filepath.Join(dir, c)
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		vals, err := ReadIni(p)
		if err != nil {
			continue
		}
		seen[key] = true
		f := IniFile{Path: p}
		f.KeyboardProvider, f.HasKbdProvider = vals["keyboardprovider"]
		f.Joystick = vals["joystick"]
		_, err = os.Stat(p + backupSuffix)
		f.HasBackup = err == nil
		f.IsReady = f.Ready()
		// MAMEのオプションらしい行が無いものは除外（フロントエンドの ini など）
		if _, ok := vals["joystick"]; !ok && !f.HasKbdProvider {
			if _, ok2 := vals["rompath"]; !ok2 {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// ReadIni は "名前 値" 形式の ini を読む。
func ReadIni(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		k, v, ok := parseLine(sc.Text())
		if ok {
			m[k] = v
		}
	}
	return m, sc.Err()
}

var lineRe = regexp.MustCompile(`^(\s*)([A-Za-z0-9_]+)(\s+)(.*?)(\s*)$`)

func parseLine(l string) (string, string, bool) {
	t := strings.TrimSpace(l)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "[") {
		return "", "", false
	}
	m := lineRe.FindStringSubmatch(l)
	if m == nil {
		f := strings.Fields(t)
		if len(f) == 1 {
			return strings.ToLower(f[0]), "", true
		}
		return "", "", false
	}
	return strings.ToLower(m[2]), m[4], true
}

// SetIni は値を書き換える。onlyExisting に含まれる項目は、元から有る場合だけ書き換える。
// 初回だけ元のファイルをバックアップする。
func SetIni(path string, vals map[string]string, onlyExisting map[string]bool, backup bool) (changed []string, err error) {
	raw, err := os.ReadFile(path)
	create := false
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		create = true
	}
	if backup && !create {
		if _, e := os.Stat(path + backupSuffix); os.IsNotExist(e) {
			if err := os.WriteFile(path+backupSuffix, raw, 0644); err != nil {
				return nil, fmt.Errorf("バックアップを作れませんでした: %w", err)
			}
		}
	}
	nl := "\n"
	if strings.Contains(string(raw), "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	done := map[string]bool{}
	for i, l := range lines {
		m := lineRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		k := strings.ToLower(m[2])
		v, ok := vals[k]
		if !ok || strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		done[k] = true
		if m[4] != v {
			lines[i] = m[1] + m[2] + m[3] + v
			changed = append(changed, k)
		}
	}
	for k, v := range vals {
		if done[k] || onlyExisting[k] {
			continue
		}
		pad := 26 - len(k)
		if pad < 1 {
			pad = 1
		}
		// 末尾の空行の前に足す
		ins := k + strings.Repeat(" ", pad) + v
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = append(lines[:n-1], ins, "")
		} else {
			lines = append(lines, ins)
		}
		changed = append(changed, k)
	}
	if len(changed) == 0 {
		return nil, nil
	}
	if create {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
	}
	out := strings.Join(lines, nl)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0644); err != nil {
		return nil, err
	}
	return changed, os.Rename(tmp, path)
}

// ApplyMame は mame.ini を「このソフト用」にする。
// keyboardprovider を dinput に（このソフトが送るキーを新しいMAMEでも読めるように）。
// joystick は 1 のまま（コントローラーはMAMEに見えたまま。ボタンとレバーの割り当ては default.cfg でキーボードだけにする。アナログ操作は使える）。
func ApplyMame(ini, provider string) ([]string, error) {
	if provider != "win32" {
		provider = "dinput"
	}
	return SetIni(ini, map[string]string{"joystick": "1", "keyboardprovider": provider},
		map[string]bool{"keyboardprovider": true, "joystick": true}, true)
}

// IniReady は mame.ini がこのソフト用になっているか。
func (f IniFile) Ready() bool {
	return !f.HasKbdProvider || f.KeyboardProvider == "dinput" || f.KeyboardProvider == "win32"
}

// RestoreMame はバックアップから戻す。
func RestoreMame(ini string) error {
	b, err := os.ReadFile(ini + backupSuffix)
	if err != nil {
		return fmt.Errorf("バックアップが見つかりません")
	}
	if err := os.WriteFile(ini, b, 0644); err != nil {
		return err
	}
	return os.Remove(ini + backupSuffix)
}

// SetGameDirect はゲーム別 ini（ini/<rom>.ini）で、そのゲームだけMAMEにパッドを直接読ませる。
func SetGameDirect(mameDir, rom string, direct bool) (string, error) {
	if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(rom) {
		return "", fmt.Errorf("ROM名が不正です: %s", rom)
	}
	p := filepath.Join(mameDir, "ini", rom+".ini")
	if direct {
		_, err := SetIni(p, map[string]string{"joystick": "1"}, nil, false)
		return p, err
	}
	vals, err := ReadIni(p)
	if err != nil {
		return p, nil // ファイルが無ければ何もしない
	}
	if len(vals) == 1 && vals["joystick"] != "" {
		return p, os.Remove(p) // このソフトが作ったものだけなら消す
	}
	_, err = SetIni(p, map[string]string{"joystick": "0"}, map[string]bool{"joystick": true}, false)
	return p, err
}

var (
	// 新しいMAME（0.2xx）：「Raiden [raiden] - MAME 0.289 (LLP64)」「… [rom] screen 1 - MAME …」
	titleNew = regexp.MustCompile(`^\s*(.*?)\s*\[([0-9a-z_]+)\](?:\s+screen\s+\d+)?\s+-\s+(\S*(?i:mame)\S*)`)
	// 古いMAME・MAMEUI：「MAME: Raiden [raiden]」「MAMEUI: …」「ThunderMAME32: …」
	titleOld = regexp.MustCompile(`^\s*([^:]*?(?i:mame)[^:]*):\s*(.*?)\s*\[([0-9a-z_]+)\]`)
	// exe の名前にmameが付くときは、形にこだわらず「説明 [rom]」を探す
	titleAny = regexp.MustCompile(`^(?:[^:\[]+:\s*)?(.*?)\s*\[([0-9a-z_]+)\]`)
)

// ParseMameTitle はMAMEのウィンドウタイトルからROM名とゲーム名を取り出す。
func ParseMameTitle(title string, exeIsMame bool) (rom, desc string, ok bool) {
	if m := titleNew.FindStringSubmatch(title); m != nil {
		return m[2], m[1], true
	}
	if m := titleOld.FindStringSubmatch(title); m != nil {
		return m[3], m[2], true
	}
	if exeIsMame {
		if m := titleAny.FindStringSubmatch(title); m != nil {
			return m[2], m[1], true
		}
	}
	return "", "", false
}

var (
	reInputTag = regexp.MustCompile(`<input\b[^>]*>`)
	rePlayers  = regexp.MustCompile(`\bplayers="(\d+)"`)
	reButtons  = regexp.MustCompile(`\bbuttons="(\d+)"`)
	reControl  = regexp.MustCompile(`<control\b[^>]*>`)
	reCtlType  = regexp.MustCompile(`\btype="([a-z0-9_]+)"`)
)

// GameInputs は -listxml から読んだ、そのゲームの人数とボタン数。
type GameInputs struct {
	Players int      `json:"players"`
	Buttons int      `json:"buttons"`
	Types   []string `json:"types"` // joy, stick, trackball, dial, paddle など
	Service bool     `json:"service"`
	Tilt    bool     `json:"tilt"`
}

// ParseListXML は mame -listxml <rom> の出力から、最初の machine/game の入力情報を取り出す。
func ParseListXML(xml string) (GameInputs, bool) {
	var g GameInputs
	in := reInputTag.FindString(xml)
	if in == "" {
		return g, false
	}
	if m := rePlayers.FindStringSubmatch(in); m != nil {
		fmt.Sscan(m[1], &g.Players)
	}
	g.Service = strings.Contains(in, `service="yes"`)
	g.Tilt = strings.Contains(in, `tilt="yes"`)
	if m := reButtons.FindStringSubmatch(in); m != nil { // 古いMAMEは input に buttons がある
		fmt.Sscan(m[1], &g.Buttons)
	}
	end := strings.Index(xml, "</input>")
	start := strings.Index(xml, in)
	if end > start {
		seen := map[string]bool{}
		for _, c := range reControl.FindAllString(xml[start:end], -1) {
			if m := reButtons.FindStringSubmatch(c); m != nil {
				var n int
				fmt.Sscan(m[1], &n)
				if n > g.Buttons {
					g.Buttons = n
				}
			}
			if m := reCtlType.FindStringSubmatch(c); m != nil && !seen[m[1]] {
				seen[m[1]] = true
				g.Types = append(g.Types, m[1])
			}
		}
	}
	return g, true
}
