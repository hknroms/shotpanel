package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFrames(t *testing.T) {
	cases := map[float64][2]int{30: {1, 1}, 8.6: {3, 4}, 3: {10, 10}, 16: {2, 2}}
	for s, want := range cases {
		on, off := Frames(s)
		if on != want[0] || off != want[1] {
			t.Errorf("%v: got %d,%d want %v", s, on, off, want)
		}
	}
}

func TestTurbo(t *testing.T) {
	// 30発/秒: 0F 押し, 1F 離し, 2F 押し
	if !TurboOn(0, 30) || TurboOn(frame, 30) || !TurboOn(2*frame, 30) {
		t.Fatal("30 pattern")
	}
	// 8.6: 3F押し 4F離し
	pat := ""
	for f := 0; f < 7; f++ {
		if TurboOn(time.Duration(f)*frame+time.Millisecond, 8.6) {
			pat += "1"
		} else {
			pat += "0"
		}
	}
	if pat != "1110000" {
		t.Fatal(pat)
	}
}

func TestEngine(t *testing.T) {
	e := NewEngine()
	prof := &DevProfile{Cfg: map[string]ButtonCfg{
		"b1": {Mode: "normal", Target: "B1"},
		"b3": {Mode: "turbo", Target: "B1", Speed: 30},
		"b5": {Mode: "normal", Target: "B1+B3"},
		"b6": {Mode: "normal", Target: "B2", Other: true},
	}}
	d := DevInput{Key: "x", Assign: 1, Map: map[string]string{"b1": "btn1", "b3": "btn3", "b5": "btn5", "b6": "btn6"}, Prof: prof,
		Controls: map[string]bool{"btn3": true, "btn5": true, "btn6": true, "left": true}}
	t0 := time.Now()
	o := e.Output(t0, []DevInput{d})
	for _, k := range []string{"1:B1", "1:B3", "2:B2", "1:LEFT"} {
		if !o[k] {
			t.Errorf("missing %s in %v", k, o)
		}
	}
	d.Controls["btn5"] = false
	o = e.Output(t0.Add(frame+time.Millisecond), []DevInput{d})
	if o["1:B1"] {
		t.Error("turbo should be off at frame 1")
	}
	// 無効のコントローラーは何も出さない
	d.Assign = 0
	if o = e.Output(t0, []DevInput{d}); len(o) != 0 {
		t.Error(o)
	}
}

func TestAlt(t *testing.T) {
	e := NewEngine()
	prof := &DevProfile{Cfg: map[string]ButtonCfg{}, Alt: AltCfg{On: true, Axis: "左右", Speed: 30}}
	d := DevInput{Key: "x", Assign: 2, Map: map[string]string{}, Prof: prof, Controls: map[string]bool{"left": true, "up": true}}
	t0 := time.Now()
	seq := ""
	for f := 0; f < 4; f++ {
		o := e.Output(t0.Add(time.Duration(f)*frame+time.Millisecond), []DevInput{d})
		if !o["2:UP"] {
			t.Fatal("up should pass through")
		}
		switch {
		case o["2:LEFT"]:
			seq += "L"
		case o["2:RIGHT"]:
			seq += "R"
		default:
			seq += "-"
		}
	}
	if seq != "L-R-" {
		t.Fatal(seq)
	}
}

func TestIni(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mame.ini")
	orig := "#\r\n# CORE INPUT OPTIONS\r\n#\r\njoystick                  1\r\nkeyboardprovider          auto\r\nrompath                   roms\r\n"
	os.WriteFile(p, []byte(orig), 0644)
	ch, err := ApplyMame(p)
	if err != nil || len(ch) != 2 {
		t.Fatal(ch, err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "joystick                  0\r\n") || !strings.Contains(string(b), "keyboardprovider          dinput\r\n") {
		t.Fatal(string(b))
	}
	fs := FindInis(dir)
	if len(fs) != 1 || !fs[0].HasBackup || fs[0].Joystick != "0" {
		t.Fatal(fs)
	}
	if err := RestoreMame(p); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != orig {
		t.Fatal("restore mismatch")
	}
	// 古いMAME（keyboardprovider 無し）は足さない
	old := filepath.Join(dir, "old.ini")
	os.WriteFile(old, []byte("joystick 1\n"), 0644)
	ApplyMame(old)
	b, _ = os.ReadFile(old)
	if string(b) != "joystick 0\n" {
		t.Fatalf("%q", b)
	}
	// ゲーム別
	gp, err := SetGameDirect(dir, "raiden", true)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := ReadIni(gp); v["joystick"] != "1" {
		t.Fatal(v)
	}
	SetGameDirect(dir, "raiden", false)
	if _, err := os.Stat(gp); !os.IsNotExist(err) {
		t.Fatal("should be removed")
	}
	if _, err := SetGameDirect(dir, "../x", true); err == nil {
		t.Fatal("bad rom accepted")
	}
}

func TestTitle(t *testing.T) {
	cases := []struct {
		in, rom, desc string
		exe            bool
	}{
		{"MAME: Raiden [raiden]", "raiden", "Raiden", false},
		{"MAMEUI: The King of Fighters '98 - The Slugfest [kof98]", "kof98", "The King of Fighters '98 - The Slugfest", false},
		{"ThunderMAME32: Gradius III (Japan, Program code R) [gradius3j]", "gradius3j", "Gradius III (Japan, Program code R)", false},
		{"Raiden [raiden]", "raiden", "Raiden", true},
	}
	for _, c := range cases {
		r, d, ok := ParseMameTitle(c.in, c.exe)
		if !ok || r != c.rom || d != c.desc {
			t.Errorf("%q -> %q %q %v", c.in, r, d, ok)
		}
	}
	if _, _, ok := ParseMameTitle("MAMEUI 0.289", true); ok {
		t.Error("frontend window matched")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	c := NewConfig(p)
	c.Games["raiden"] = &GameProfile{Title: "雷電", Devices: map[string]*DevProfile{"d": DefaultProfile("8b")}}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p)
	if err != nil || c2.Games["raiden"].Devices["d"].Cfg["b5"].Target != "B4" {
		t.Fatal(err)
	}
	if DefaultMap("hid", SonyVID, 14)["b1"] != "btn1" || DefaultMap("hid", SonyVID, 14)["b5"] != "btn2" {
		t.Fatal("ps map")
	}
}

func TestMameKeys(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mame.ini"), []byte("joystick 0\ncfg_directory cfg\n"), 0644)
	// 無い状態から作る
	keys := DefaultKeys()
	p, err := WriteMameKeys(dir, keys)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, `<port type="P2_BUTTON6">`) || !strings.Contains(s, "KEYCODE_H") || strings.Contains(s, "P1_BUTTON1") {
		t.Fatal(s)
	}
	mk, _ := ReadMameKeys(dir)
	if mk.Keys["2"]["B6"] != "H" || mk.Keys["1"]["B1"] != "LCONTROL" || !mk.HasBackup {
		t.Fatal(mk.Keys)
	}
	// MAMEが書いたファイル（ほかの設定あり）に書き足す
	orig := `<?xml version="1.0"?>
<mameconfig version="10">
    <system name="default">
        <input>
            <port type="UI_PAUSE">
                <newseq type="standard">KEYCODE_F11</newseq>
            </port>
            <port type="P1_BUTTON1">
                <newseq type="standard">KEYCODE_A OR JOYCODE_1_BUTTON1</newseq>
            </port>
        </input>
    </system>
</mameconfig>
`
	os.Remove(p + backupSuffix)
	os.WriteFile(p, []byte(orig), 0644)
	os.WriteFile(filepath.Join(dir, "cfg", "raiden.cfg"), []byte(`<mameconfig version="10"><system name="raiden"><input><port tag=":IN0" type="P1_BUTTON1" mask="1" defvalue="1"><newseq type="standard">KEYCODE_B</newseq></port></input></system></mameconfig>`), 0644)
	mk, _ = ReadMameKeys(dir)
	if mk.Keys["1"]["B1"] != "A" || len(mk.Overrides) != 1 || mk.Overrides[0] != "raiden" {
		t.Fatal(mk.Keys["1"], mk.Overrides)
	}
	keys["1"]["B1"] = "LCONTROL"
	keys["1"]["B2"] = "C"
	WriteMameKeys(dir, keys)
	b, _ = os.ReadFile(p)
	s = string(b)
	if !strings.Contains(s, "UI_PAUSE") || strings.Contains(s, "KEYCODE_A OR") || !strings.Contains(s, "KEYCODE_C") {
		t.Fatal(s)
	}
	mk, _ = ReadMameKeys(dir)
	if mk.Keys["1"]["B1"] != "LCONTROL" || mk.Keys["1"]["B2"] != "C" {
		t.Fatal(mk.Keys["1"])
	}
	if err := RestoreMameKeys(dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != orig {
		t.Fatal("restore")
	}
	if PortType("2", "COIN") != "COIN2" || PortType("1", "UP") != "P1_JOYSTICK_UP" || PortType("2", "B6") != "P2_BUTTON6" {
		t.Fatal("port names")
	}
}

func TestCustomPattern(t *testing.T) {
	e := NewEngine()
	prof := &DevProfile{Cfg: map[string]ButtonCfg{"b1": {Mode: "turbo", Target: "B1", Speed: 30, On: 2, Off: 1}}}
	d := DevInput{Key: "x", Assign: 1, Map: map[string]string{"b1": "btn1"}, Prof: prof, Controls: map[string]bool{"btn1": true}}
	t0 := time.Now()
	pat := ""
	for f := 0; f < 6; f++ {
		if e.Output(t0.Add(time.Duration(f)*frame+time.Millisecond), []DevInput{d})["1:B1"] {
			pat += "1"
		} else {
			pat += "0"
		}
	}
	if pat != "110110" {
		t.Fatal(pat)
	}
}

func TestSeqKeys(t *testing.T) {
	cases := map[string]string{
		"KEYCODE_F6 KEYCODE_LSHIFT OR KEYCODE_F6 KEYCODE_RSHIFT": "LSHIFT+F6",
		"KEYCODE_F5 NOT KEYCODE_LSHIFT NOT KEYCODE_RSHIFT":       "F5",
		"KEYCODE_LCONTROL OR JOYCODE_1_BUTTON1":                  "LCONTROL",
		"JOYCODE_1_BUTTON1":                                      "",
	}
	for in, want := range cases {
		if got := seqKeys(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	dir := t.TempDir()
	keys := DefaultKeys()
	keys["0"]["TILT"] = "Y"
	p, _ := WriteMameKeys(dir, keys)
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, `"P2_BUTTON7"`) || !strings.Contains(s, "KEYCODE_Y") || strings.Contains(s, "UI_MENU") {
		t.Fatal(s)
	}
	mk, _ := ReadMameKeys(dir)
	if mk.Keys["0"]["TILT"] != "Y" || mk.Keys["2"]["B10"] != "U" || mk.Keys["0"]["QSAVE"] != "LSHIFT+F6" {
		t.Fatal(mk.Keys)
	}
}

func TestListXML(t *testing.T) {
	newer := `<mame build="0.289"><machine name="raiden" sourcefile="seibu/raiden.cpp"><description>Raiden</description>
	<input players="2" coins="2" service="yes"><control type="joy" player="1" buttons="2" ways="8"/><control type="joy" player="2" buttons="2" ways="8"/></input></machine></mame>`
	g, ok := ParseListXML(newer)
	if !ok || g.Players != 2 || g.Buttons != 2 || len(g.Types) != 1 || g.Types[0] != "joy" || !g.Service || g.Tilt {
		t.Fatal(g)
	}
	older := `<game name="sf2"><input players="2" buttons="6" coins="2"><control type="joy8way"/></input></game>`
	g, ok = ParseListXML(older)
	if !ok || g.Buttons != 6 || g.Types[0] != "joy8way" {
		t.Fatal(g)
	}
}
