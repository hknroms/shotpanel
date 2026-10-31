package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequestsForPlugin(t *testing.T) {
	e := NewEngine()
	prof := &DevProfile{Cfg: map[string]ButtonCfg{
		"b1": {Mode: "normal", Target: "B1"},
		"b3": {Mode: "turbo", Target: "B1", Speed: 30},
		"b4": {Mode: "turbo", Target: "B2", On: 2, Off: 3},
		"b5": {Mode: "normal", Target: "SYS:SERVICE1+SYS:PAUSE"},
		"b6": {Mode: "normal", Target: "START", Other: true},
	}}
	d := DevInput{Key: "x", Assign: 1, Map: map[string]string{"b1": "c1", "b3": "c3", "b4": "c4", "b5": "c5", "b6": "c6"}, Prof: prof,
		Controls: map[string]bool{"c3": true, "c4": true, "c5": true, "c6": true, "up": true}}
	t0 := time.Now()
	reqs := e.Requests(t0, []DevInput{d})
	plug, keys := SplitForPlugin(reqs)
	got := map[string]PluginReq{}
	for _, r := range plug {
		got[strings.Join(r.A, "+")] = r
	}
	if r := got["P1_BUTTON1"]; r.On != 1 || r.Off != 1 {
		t.Errorf("turbo 30: %+v", r)
	}
	if r := got["P1_BUTTON2"]; r.On != 2 || r.Off != 3 {
		t.Errorf("turbo 2/3: %+v", r)
	}
	if r, ok := got["SERVICE1"]; !ok || r.Off != 0 {
		t.Errorf("service: %+v %v", r, got)
	}
	if _, ok := got["START2"]; !ok {
		t.Errorf("other player start: %v", got)
	}
	if _, ok := got["P1_JOYSTICK_UP"]; !ok {
		t.Errorf("lever: %v", got)
	}
	// 一時停止は MAME の操作なのでキーで送る
	if len(keys) != 1 || len(keys[0].A) != 1 || keys[0].A[0] != "1:SYS:PAUSE" {
		t.Errorf("keys: %+v", keys)
	}
	// 押し続けている間は番号が変わらない。離して押し直すと変わる
	n := got["P1_BUTTON1"].N
	reqs = e.Requests(t0.Add(50*time.Millisecond), []DevInput{d})
	plug, _ = SplitForPlugin(reqs)
	for _, r := range plug {
		if r.A[0] == "P1_BUTTON1" && r.N != n {
			t.Error("number changed while held")
		}
	}
	d.Controls["c3"] = false
	e.Requests(t0.Add(60*time.Millisecond), []DevInput{d})
	d.Controls["c3"] = true
	plug, _ = SplitForPlugin(e.Requests(t0.Add(70*time.Millisecond), []DevInput{d}))
	for _, r := range plug {
		if r.A[0] == "P1_BUTTON1" && r.N == n {
			t.Error("number should change on new press")
		}
	}
}

func TestAltForPlugin(t *testing.T) {
	e := NewEngine()
	prof := &DevProfile{Cfg: map[string]ButtonCfg{}, Alt: AltCfg{On: true, Axis: "左右", Speed: 15}}
	d := DevInput{Key: "x", Assign: 2, Map: map[string]string{}, Prof: prof, Controls: map[string]bool{"left": true}}
	plug, _ := SplitForPlugin(e.Requests(time.Now(), []DevInput{d}))
	if len(plug) != 1 || plug[0].A[0] != "P2_JOYSTICK_LEFT" || plug[0].B[0] != "P2_JOYSTICK_RIGHT" || plug[0].On != 2 || plug[0].Off != 2 {
		t.Fatalf("%+v", plug)
	}
}

func TestEncodeState(t *testing.T) {
	b := EncodeState(7, []PluginReq{{N: 3, On: 1, Off: 1, A: []string{"P1_BUTTON1", "P1_BUTTON2"}}, {N: 4, On: 1, A: []string{"START1"}},
		{N: 5, On: 2, Off: 2, A: []string{"P2_JOYSTICK_LEFT"}, B: []string{"P2_JOYSTICK_RIGHT"}}})
	if len(b) != StateSize {
		t.Fatal(len(b))
	}
	want := "SHOTPANEL 1 7\nR 3 1 1 P1_BUTTON1,P1_BUTTON2 -\nR 4 1 0 START1 -\nR 5 2 2 P2_JOYSTICK_LEFT P2_JOYSTICK_RIGHT\nE 7\n"
	if s := strings.TrimRight(string(b), " "); s != want {
		t.Fatalf("%q", s)
	}
	// 多すぎても StateSize に収まり、終わりの印がある
	var many []PluginReq
	for i := 0; i < 500; i++ {
		many = append(many, PluginReq{N: uint64(i), On: 1, Off: 1, A: []string{"P1_BUTTON10"}})
	}
	b = EncodeState(9, many)
	if len(b) != StateSize || !strings.Contains(string(b), "\nE 9\n") {
		t.Fatal("overflow")
	}
}

func TestInstallPlugin(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mameui64.exe"), nil, 0644)
	os.WriteFile(filepath.Join(dir, "mame.ini"), []byte("plugins                   0\r\nkeyboardprovider          win32\r\n"), 0644)
	if p := GetPluginInfo(dir); p.Ready || p.Boot || p.Installed || p.Enabled {
		t.Fatalf("%+v", p)
	}
	if _, err := InstallPlugin(dir); err != nil {
		t.Fatal(err)
	}
	p := GetPluginInfo(dir)
	if !p.Ready {
		t.Fatalf("%+v", p)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ini", "plugin.ini")); !strings.Contains(string(b), "shotpanel") {
		t.Fatalf("plugin.ini: %q", b)
	}
	if b, _ := os.ReadFile(StatePath(dir)); len(b) != StateSize {
		t.Fatal("state.txt")
	}
	// 2回目も大丈夫
	if _, err := InstallPlugin(dir); err != nil {
		t.Fatal(err)
	}
	if err := UninstallPlugin(dir); err != nil {
		t.Fatal(err)
	}
	if fileExists(PluginDir(dir)) || fileExists(filepath.Join(dir, "plugins", "boot.lua")) || fileExists(filepath.Join(dir, "ini", "plugin.ini")) {
		t.Fatal("not removed")
	}
}

func TestInstallPluginKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plugins"), 0755)
	os.WriteFile(filepath.Join(dir, "plugins", "boot.lua"), []byte("-- original"), 0644)
	os.MkdirAll(filepath.Join(dir, "ini"), 0755)
	os.WriteFile(filepath.Join(dir, "ini", "plugin.ini"), []byte("autofire                  1\r\nshotpanel                 0\r\n"), 0644)
	if p := GetPluginInfo(dir); p.Enabled {
		t.Fatal("shotpanel 0 should be disabled")
	}
	if _, err := InstallPlugin(dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "ini", "plugin.ini"))
	if !strings.Contains(string(b), "autofire                  1\r\n") || !strings.Contains(string(b), "shotpanel                 1") {
		t.Fatalf("%q", b)
	}
	UninstallPlugin(dir)
	if b, _ := os.ReadFile(filepath.Join(dir, "plugins", "boot.lua")); string(b) != "-- original" {
		t.Fatal("boot.lua must be kept")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ini", "plugin.ini")); !strings.Contains(string(b), "shotpanel                 0") {
		t.Fatalf("restore: %q", b)
	}
}

func TestParseAck(t *testing.T) {
	a, ok := ParseAck([]byte("raiden 12 340 57.613\n"))
	if !ok || a.Rom != "raiden" || a.Seq != 12 || a.Frames != 340 || a.Hz != 57.613 {
		t.Fatalf("%+v", a)
	}
	if _, ok := ParseAck([]byte("raiden 0")); ok {
		t.Fatal("short")
	}
}
