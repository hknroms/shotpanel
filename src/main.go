//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jchv/go-webview2"

	"shotpanel/core"
	"shotpanel/win"
)

//go:embed ui/index.html
var indexHTML string

const appName = "連射パネル設定"

type gameInfo struct {
	Rom   string           `json:"rom"`
	Title string           `json:"title"`
	Exe   string           `json:"exe"`
	Front bool             `json:"front"`
	Info  *core.GameInputs `json:"info,omitempty"`
	pid   uint32
}

type App struct {
	cfg    *core.Config
	in     *win.Input
	eng    *core.Engine
	tray   *win.Tray
	wv     webview2.WebView
	hwnd   uintptr
	exeDir string

	mu       sync.Mutex
	game     gameInfo
	paused   bool
	active   bool
	sent     map[string]bool
	saveTmr  *time.Timer
	devDirty bool
	quit     chan struct{}
	info     map[string]*core.GameInputs // ROM名 → MAMEの -listxml から読んだ入力情報
}

func main() {
	if !win.SingleInstance() {
		return
	}
	exe, _ := os.Executable()
	a := &App{exeDir: filepath.Dir(exe), eng: core.NewEngine(), sent: map[string]bool{}, quit: make(chan struct{})}
	var err error
	a.cfg, err = core.Load(core.ConfigPath(a.exeDir))
	if err != nil {
		win.MessageBox(appName, "設定ファイルを読めなかったので、初期設定で起動します。\n"+err.Error())
	}
	a.in = win.NewInput()
	a.in.OnChange = func() { a.mu.Lock(); a.devDirty = true; a.mu.Unlock() }
	a.tray = &win.Tray{
		OnOpen:  a.show,
		OnPause: func() { a.mu.Lock(); a.paused = !a.paused; a.mu.Unlock() },
		OnQuit:  a.exit,
		Paused:  func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.paused },
	}
	a.in.Start(func(h uintptr) { a.tray.Add(h, appName) })
	win.TimeBeginPeriod()
	go a.loop()

	startHidden := false
	for _, s := range os.Args[1:] {
		if strings.EqualFold(s, "/tray") || strings.EqualFold(s, "-tray") {
			startHidden = true
		}
	}
	a.runUI(startHidden)
}

func (a *App) runUI(hidden bool) {
	sc := win.Scale()
	a.wv = webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(os.Getenv("LOCALAPPDATA"), "ShotPanel", "WebView2"),
		WindowOptions: webview2.WindowOptions{
			Title: appName, Width: uint(720 * sc), Height: uint(880 * sc), Center: true, IconId: 2,
		},
	})
	if a.wv == nil {
		win.MessageBox(appName, "画面の表示に必要な「Microsoft Edge WebView2 ランタイム」が見つかりません。\n"+
			"Microsoft のサイトからインストールしてください。\n\n連射と割り当ては、通知領域のアイコンで常駐したまま動き続けます。")
		<-a.quit
		return
	}
	a.hwnd = uintptr(a.wv.Window())
	win.HookClose(a.hwnd, func() { win.ShowWindow(a.hwnd, false) })
	a.wv.Bind("api", a.api)
	a.wv.SetHtml(indexHTML)
	if hidden {
		win.ShowWindow(a.hwnd, false)
	}
	a.wv.Run()
	a.wv.Destroy()
}

func (a *App) show() {
	if a.hwnd != 0 {
		win.ShowWindow(a.hwnd, true)
	}
}

func (a *App) exit() {
	a.mu.Lock()
	for k := range a.sent {
		win.SendKey(k, false)
	}
	a.sent = map[string]bool{}
	a.mu.Unlock()
	a.flushSave()
	a.tray.Remove()
	os.Exit(0)
}

// ---- 入力 → キー送信のループ ----

func (a *App) loop() {
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	var lastXI, lastFind, lastPush time.Time
	ownPID := uint32(os.Getpid())
	for now := range tick.C {
		if now.Sub(lastXI) >= 4*time.Millisecond {
			if a.in.PollXInput(now) {
				a.mu.Lock()
				a.devDirty = true
				a.mu.Unlock()
			}
			lastXI = now
		}
		if now.Sub(lastFind) >= 250*time.Millisecond {
			a.detectMame()
			lastFind = now
		}
		a.cfg.Lock()
		devs := a.in.Snapshot(func(key string) bool {
			if d := a.cfg.Devices[key]; d != nil {
				return d.StickAsLever
			}
			return true
		})
		added := a.ensureDevices(devs)
		a.mu.Lock()
		g := a.game
		paused := a.paused
		if added {
			a.devDirty = true
		}
		a.mu.Unlock()
		prof := a.cfg.Games[core.Common]
		if gp := a.cfg.Games[g.Rom]; g.Rom != "" && gp != nil {
			prof = gp
		}
		fg := win.ForegroundPID()
		active := !paused && !prof.Direct && fg != ownPID
		if a.cfg.Options.OnlyWhenFront {
			active = active && g.Rom != "" && fg == g.pid
		}
		var ins []core.DevInput
		if active {
			for _, d := range devs {
				ds := a.cfg.Devices[d.Key]
				ins = append(ins, core.DevInput{Key: d.Key, Assign: ds.Assign, Controls: d.Pressed, Map: ds.Map, Prof: a.profileFor(prof, d.Key, ds.Layout)})
			}
		}
		want := map[string]bool{}
		for k := range a.eng.Output(now, ins) {
			pl, fn, _ := strings.Cut(k, ":")
			if strings.HasPrefix(fn, "SYS:") {
				pl, fn = "0", fn[4:]
			}
			if key := a.cfg.Keys[pl][fn]; key != "" {
				for _, x := range strings.Split(key, "+") {
					want[x] = true
				}
			}
		}
		a.cfg.Unlock()

		a.mu.Lock()
		for k := range a.sent {
			if !want[k] {
				win.SendKey(k, false)
				delete(a.sent, k)
			}
		}
		for k := range want {
			if !a.sent[k] {
				win.SendKey(k, true)
				a.sent[k] = true
			}
		}
		a.active = active
		dirty := a.devDirty
		a.devDirty = false
		a.mu.Unlock()

		if a.hwnd != 0 && (dirty || now.Sub(lastPush) >= 33*time.Millisecond) && win.IsVisible(a.hwnd) {
			a.push(devs, dirty)
			lastPush = now
		}
	}
}

func (a *App) profileFor(g *core.GameProfile, key, layout string) *core.DevProfile {
	if p := g.Devices[key]; p != nil {
		return p
	}
	if p := a.cfg.Games[core.Common].Devices[key]; p != nil {
		return p
	}
	return core.DefaultProfile(layout)
}

// ensureDevices は初めて見るコントローラーを設定に足す（cfg をロック中に呼ぶ）。
func (a *App) ensureDevices(devs []win.DevState) bool {
	added := false
	used := map[int]bool{}
	for _, d := range devs {
		if ds := a.cfg.Devices[d.Key]; ds != nil {
			used[ds.Assign] = true
		}
	}
	for _, d := range devs {
		ds := a.cfg.Devices[d.Key]
		if ds == nil {
			assign := 0
			if !used[1] {
				assign = 1
			} else if !used[2] {
				assign = 2
			}
			used[assign] = true
			order := 0
			for _, x := range a.cfg.Devices {
				if x.Order >= order {
					order = x.Order + 1
				}
			}
			ds = &core.DevSettings{Name: d.Name, Assign: assign, Order: order,
				Layout: core.DefaultLayout(d.Name, d.Kind, d.VID), Phys: core.DefaultPhys(d.Kind),
				Map: core.DefaultMap(d.Kind, d.VID, d.NButtons), StickAsLever: true}
			a.cfg.Devices[d.Key] = ds
			added = true
		}
		// 位置が決まっていない入力は「その他」に足す
		mapped := map[string]bool{}
		for _, c := range ds.Map {
			mapped[c] = true
		}
		for _, c := range d.Controls {
			if c == "up" || c == "down" || c == "left" || c == "right" || mapped[c] {
				continue
			}
			ds.Map["x_"+c] = c
			added = true
		}
	}
	if added {
		a.scheduleSave()
	}
	return added
}

func (a *App) detectMame() {
	w := win.FindMame(func(t string, isMame bool) bool { _, _, ok := core.ParseMameTitle(t, isMame); return ok })
	g := gameInfo{}
	if w != nil {
		rom, desc, _ := core.ParseMameTitle(w.Title, true)
		g = gameInfo{Rom: rom, Title: desc, Exe: w.Exe, pid: w.PID, Front: win.ForegroundPID() == w.PID}
	}
	a.mu.Lock()
	changed := g.Rom != a.game.Rom
	if g.Rom != "" {
		if a.info == nil {
			a.info = map[string]*core.GameInputs{}
		}
		if inf, ok := a.info[g.Rom]; ok {
			g.Info = inf
		} else if changed {
			a.info[g.Rom] = nil
			go a.lookupInputs(g.Rom, g.Exe)
		}
	}
	a.game = g
	if changed {
		a.devDirty = true
	}
	a.mu.Unlock()
	if changed {
		tip := appName
		if g.Rom != "" {
			tip += "：" + g.Rom
		}
		a.tray.SetTip(tip)
	}
}

type devOut struct {
	*win.Device
	Down []string `json:"down"`
}

func (a *App) push(devs []win.DevState, withConfig bool) {
	a.mu.Lock()
	var sent []string
	for k := range a.sent {
		sent = append(sent, k)
	}
	sort.Strings(sent)
	st := map[string]interface{}{"game": a.game, "active": a.active, "paused": a.paused, "sent": sent}
	a.mu.Unlock()
	var list []devOut
	for _, d := range devs {
		list = append(list, devOut{d.Device, d.Down})
	}
	st["devices"] = list
	if withConfig {
		a.cfg.Lock()
		st["config"] = a.cfg
		b, _ := json.Marshal(st)
		a.cfg.Unlock()
		a.eval(b)
		return
	}
	b, _ := json.Marshal(st)
	a.eval(b)
}

func (a *App) eval(b []byte) {
	s := string(b)
	a.wv.Dispatch(func() { a.wv.Eval("window.onState && window.onState(" + s + ")") })
}

// ---- 保存 ----

func (a *App) scheduleSave() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.saveTmr != nil {
		a.saveTmr.Stop()
	}
	a.saveTmr = time.AfterFunc(400*time.Millisecond, a.flushSave)
}

func (a *App) flushSave() {
	a.cfg.Lock()
	err := a.cfg.Save()
	a.cfg.Unlock()
	if err != nil && a.wv != nil {
		a.wv.Dispatch(func() {
			a.wv.Eval(fmt.Sprintf("window.toast && toast(%q)", "設定を保存できませんでした: "+err.Error()))
		})
	}
}

// ---- 画面からの呼び出し ----

func (a *App) api(cmd string, arg string) (interface{}, error) {
	switch cmd {
	case "init":
		a.mu.Lock()
		a.devDirty = true
		a.mu.Unlock()
		return map[string]interface{}{"keyNames": sortedKeys(), "autostart": win.Autostart()}, nil
	case "putConfig":
		var c core.Config
		if err := json.Unmarshal([]byte(arg), &c); err != nil {
			return nil, err
		}
		a.cfg.Lock()
		for k, d := range c.Devices {
			a.cfg.Devices[k] = d
		}
		if c.Games != nil {
			if c.Games[core.Common] == nil {
				c.Games[core.Common] = a.cfg.Games[core.Common]
			}
			a.cfg.Games = c.Games
		}
		a.cfg.Options = c.Options
		if c.Keys != nil {
			a.cfg.Keys = c.Keys
		}
		a.cfg.Unlock()
		a.scheduleSave()
		return true, nil
	case "forgetDevice":
		a.cfg.Lock()
		delete(a.cfg.Devices, arg)
		for _, g := range a.cfg.Games {
			delete(g.Devices, arg)
		}
		a.cfg.Unlock()
		a.scheduleSave()
		a.mu.Lock()
		a.devDirty = true
		a.mu.Unlock()
		return true, nil
	case "pause":
		a.mu.Lock()
		a.paused = arg == "1"
		a.mu.Unlock()
		return true, nil
	case "hide":
		win.ShowWindow(a.hwnd, false)
		return true, nil
	case "quit":
		go a.exit()
		return true, nil
	case "autostart":
		return true, win.SetAutostart(arg == "1")
	case "mameInfo":
		dir := arg
		a.mu.Lock()
		running := a.game.Exe
		a.mu.Unlock()
		if dir == "" && running != "" {
			dir = filepath.Dir(running)
		}
		res := map[string]interface{}{"dir": dir, "running": running != ""}
		if running != "" {
			res["runningDir"] = filepath.Dir(running)
		}
		if dir != "" {
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				return nil, fmt.Errorf("フォルダが見つかりません: %s", dir)
			}
			if core.MameUIExe(dir) == "" {
				return nil, fmt.Errorf("このフォルダに mameui64.exe（MAMEUI）がありません。MAMEUIのフォルダを選んでください: %s", dir)
			}
			res["inis"] = core.FindInis(dir)
		}
		return res, nil
	case "mameApply", "mameRestore":
		var p struct{ Dir, Path string }
		json.Unmarshal([]byte(arg), &p)
		if err := a.mameNotRunning(); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(strings.ToLower(filepath.Clean(p.Path)), strings.ToLower(filepath.Clean(p.Dir))) {
			return nil, fmt.Errorf("MAMEのフォルダの外のファイルは書き換えません")
		}
		if cmd == "mameRestore" {
			return true, core.RestoreMame(p.Path)
		}
		ch, err := core.ApplyMame(p.Path, a.cfg.Options.KbdProvider)
		return ch, err
	case "pickFolder":
		dir, err := win.PickFolder(a.hwnd, "MAMEUIのフォルダ（mameui64.exe がある所）を選んでください", arg)
		if err == win.ErrCanceled {
			return "", nil
		}
		return dir, err
	case "mameSetup", "mameUndo":
		var p struct{ Dir string }
		json.Unmarshal([]byte(arg), &p)
		if p.Dir == "" {
			return nil, fmt.Errorf("先にMAMEのフォルダを選んでください")
		}
		if err := a.mameNotRunning(); err != nil {
			return nil, err
		}
		if core.MameUIExe(p.Dir) == "" {
			return nil, fmt.Errorf("このフォルダに mameui64.exe（MAMEUI）がありません")
		}
		inis := core.FindInis(p.Dir)
		var done []string
		if cmd == "mameUndo" {
			for _, f := range inis {
				if f.HasBackup {
					if err := core.RestoreMame(f.Path); err != nil {
						return nil, err
					}
					done = append(done, f.Path)
				}
			}
			if err := core.RestoreMameKeys(p.Dir); err == nil {
				done = append(done, "default.cfg")
			}
			return done, nil
		}
		a.cfg.Lock()
		prov := a.cfg.Options.KbdProvider
		a.cfg.Unlock()
		done, err := core.MameUISetup(p.Dir, prov)
		if err != nil {
			return nil, err
		}
		a.cfg.Lock()
		keys := a.cfg.Keys
		a.cfg.Unlock()
		cp, err := core.WriteMameKeys(p.Dir, keys)
		if err != nil {
			return nil, err
		}
		return append(done, cp), nil
	case "mameKeys":
		if arg == "" {
			return nil, fmt.Errorf("先にMAMEのフォルダを指定してください")
		}
		return core.ReadMameKeys(arg)
	case "mameKeysWrite", "mameKeysRestore":
		var p struct {
			Dir  string
			Keys map[string]map[string]string
		}
		json.Unmarshal([]byte(arg), &p)
		if p.Dir == "" {
			return nil, fmt.Errorf("先にMAMEのフォルダを指定してください")
		}
		if err := a.mameNotRunning(); err != nil {
			return nil, err
		}
		if cmd == "mameKeysRestore" {
			return true, core.RestoreMameKeys(p.Dir)
		}
		return core.WriteMameKeys(p.Dir, p.Keys)
	case "gameDirect":
		var p struct {
			Dir, Rom string
			On       bool
		}
		json.Unmarshal([]byte(arg), &p)
		if p.Dir == "" {
			return nil, fmt.Errorf("先に「MAMEの設定」でMAMEのフォルダを指定してください")
		}
		if err := a.mameNotRunning(); err != nil {
			return nil, err
		}
		path, err := core.SetGameDirect(p.Dir, p.Rom, p.On)
		return path, err
	}
	return nil, fmt.Errorf("不明な操作: %s", cmd)
}

func (a *App) mameNotRunning() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.game.Rom != "" {
		return fmt.Errorf("MAMEが起動中です。MAMEを閉じてから書き込んでください（MAMEUIは閉じるときに設定を書き戻すことがあります）")
	}
	if win.MameAppOpen() {
		return fmt.Errorf("MAMEUI（またはMAME）が開いています。MAMEUIは閉じるときに設定を書き戻して、書き換えを消してしまうので、完全に閉じてから押してください")
	}
	return nil
}

func sortedKeys() []string {
	k := win.KeyNames()
	sort.Strings(k)
	return k
}

// lookupInputs は「mame -listxml ROM名」で、そのゲームの人数とボタン数を調べる。
func (a *App) lookupInputs(rom, exe string) {
	orig := exe
	if exe == "" {
		return
	}
	// MAMEUI など画面付きのものは避けて、同じフォルダのコマンド版を使う
	if strings.Contains(strings.ToLower(filepath.Base(exe)), "ui") {
		exe = ""
		for _, n := range []string{"mame.exe", "mame64.exe", "mamearcade64.exe"} {
			p := filepath.Join(filepath.Dir(orig), n)
			if _, err := os.Stat(p); err == nil {
				exe = p
				break
			}
		}
		if exe == "" {
			return
		}
	}
	out, err := win.RunHidden(exe, filepath.Dir(exe), 15*time.Second, "-listxml", rom)
	if err != nil {
		return
	}
	g, ok := core.ParseListXML(out)
	if !ok {
		return
	}
	a.mu.Lock()
	a.info[rom] = &g
	if a.game.Rom == rom {
		a.game.Info = &g
		a.devDirty = true
	}
	a.mu.Unlock()
}
