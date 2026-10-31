// Package core はOSに依存しない部分（設定・連射エンジン・mame.ini 操作）。
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ButtonCfg は1つの物理ボタンの割り当て。
type ButtonCfg struct {
	Mode   string  `json:"mode"`          // normal / turbo / off
	Target string  `json:"target"`        // B1..B6, START, COIN, "B1+B3" など
	Other  bool    `json:"other"`         // もう一方のプレイヤーへ出力（拡張）
	Speed  float64 `json:"speed"`         // 連射速度（発/秒）
	On     int     `json:"on,omitempty"`  // 押すフレーム数（0なら速度から決める）
	Off    int     `json:"off,omitempty"` // 離すフレーム数
}

// AltCfg はレバーの交互連打。
type AltCfg struct {
	On    bool    `json:"on"`
	Axis  string  `json:"axis"` // 左右 / 上下
	Speed float64 `json:"speed"`
	POn   int     `json:"pon,omitempty"`  // 押すフレーム数
	POff  int     `json:"poff,omitempty"` // 離すフレーム数
}

// DevProfile はゲームごと・コントローラーごとのボタン設定。
type DevProfile struct {
	Preset string               `json:"preset"`
	Cfg    map[string]ButtonCfg `json:"cfg"`
	Alt    AltCfg               `json:"alt"`
}

// GameProfile は1ゲーム分の設定。
type GameProfile struct {
	Title   string                 `json:"title,omitempty"`
	Devices map[string]*DevProfile `json:"devices"`
	Direct  bool                   `json:"direct"` // true: MAMEにパッドを直接読ませる（このソフトは送信しない）
}

// DevSettings はコントローラー自体の設定（ゲームに依存しない）。
type DevSettings struct {
	Name         string            `json:"name"`
	Assign       int               `json:"assign"` // 1, 2, 0=無効
	Order        int               `json:"order"`
	Layout       string            `json:"layout"` // 8b / 6b / 3b / pad
	Phys         string            `json:"phys"`   // ps / xbox
	Map          map[string]string `json:"map"`    // 位置 → 物理入力ID
	StickAsLever bool              `json:"stickAsLever"`
}

// Options は全体設定。
type Options struct {
	OnlyWhenFront bool   `json:"onlyWhenFront"`
	MameDir       string `json:"mameDir"`
	KbdProvider   string `json:"kbdProvider"` // dinput（標準）/ win32
	Output        string `json:"output"`      // MAMEへの送り方："key"（キー送信。空も同じ）/ "plugin"（MAMEのプラグインでフレームに合わせる）
}

// UsePlugin は MAME のプラグインで送る設定か。
func (o Options) UsePlugin() bool { return o.Output == "plugin" }

// Config はファイルに保存する全体。
type Config struct {
	Devices map[string]*DevSettings      `json:"devices"`
	Games   map[string]*GameProfile      `json:"games"` // "_common" = 共通設定
	Options Options                      `json:"options"`
	Keys    map[string]map[string]string `json:"keys"` // "1"/"2" → 機能 → キー名

	mu   sync.Mutex
	path string
}

const Common = "_common"

func DefaultKeys() map[string]map[string]string {
	return map[string]map[string]string{
		"1": {"UP": "UP", "DOWN": "DOWN", "LEFT": "LEFT", "RIGHT": "RIGHT",
			"B1": "LCONTROL", "B2": "LALT", "B3": "SPACE", "B4": "LSHIFT", "B5": "Z", "B6": "X",
			"B7": "C", "B8": "V", "B9": "B", "B10": "N",
			"START": "1", "COIN": "5"},
		"2": {"UP": "R", "DOWN": "F", "LEFT": "D", "RIGHT": "G",
			"B1": "A", "B2": "S", "B3": "Q", "B4": "W", "B5": "E", "B6": "H",
			"B7": "J", "B8": "K", "B9": "L", "B10": "U",
			"START": "2", "COIN": "6"},
		// 共通（サービスなど）とMAMEの操作。MAME 0.289 の初期値。
		"0": {"SERVICE1": "9", "SERVICE": "F2", "TILT": "T",
			"MENU": "TAB", "PAUSE": "F5", "RESET": "F3", "FFWD": "INSERT",
			"QSAVE": "LSHIFT+F6", "QLOAD": "LSHIFT+F7", "EXIT": "ESC"},
	}
}

func NewConfig(path string) *Config {
	return &Config{
		Devices: map[string]*DevSettings{},
		Games:   map[string]*GameProfile{Common: {Devices: map[string]*DevProfile{}}},
		Options: Options{OnlyWhenFront: true},
		Keys:    DefaultKeys(),
		path:    path,
	}
}

// Load は設定を読む。無ければ初期値。
func Load(path string) (*Config, error) {
	c := NewConfig(path)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return NewConfig(path), err
	}
	c.path = path
	if c.Devices == nil {
		c.Devices = map[string]*DevSettings{}
	}
	if c.Games == nil {
		c.Games = map[string]*GameProfile{}
	}
	if c.Games[Common] == nil {
		c.Games[Common] = &GameProfile{Devices: map[string]*DevProfile{}}
	}
	def := DefaultKeys()
	// v0.1 の誤った初期値（2PのB5・B6 = I・K）を直す
	if k2 := c.Keys["2"]; k2 != nil && k2["B5"] == "I" && k2["B6"] == "K" {
		k2["B5"], k2["B6"] = "E", "H"
	}
	if c.Keys == nil {
		c.Keys = def
	}
	for pl, m := range def {
		if c.Keys[pl] == nil {
			c.Keys[pl] = map[string]string{}
		}
		for k, v := range m {
			if _, ok := c.Keys[pl][k]; !ok {
				c.Keys[pl][k] = v
			}
		}
	}
	return c, nil
}

func (c *Config) Lock()   { c.mu.Lock() }
func (c *Config) Unlock() { c.mu.Unlock() }

// Save は呼び出し側でロックしている前提。一時ファイル経由で書く。
func (c *Config) Save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *Config) Path() string { return c.path }

// GameList は保存済みゲーム名の一覧（共通設定を除く）。
func (c *Config) GameList() []map[string]string {
	var out []map[string]string
	for k, g := range c.Games {
		if k == Common {
			continue
		}
		out = append(out, map[string]string{"rom": k, "title": g.Title})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["rom"] < out[j]["rom"] })
	return out
}

// CloneProfile はディープコピー。
func CloneProfile(g *GameProfile) *GameProfile {
	if g == nil {
		return &GameProfile{Devices: map[string]*DevProfile{}}
	}
	b, _ := json.Marshal(g)
	var out GameProfile
	_ = json.Unmarshal(b, &out)
	if out.Devices == nil {
		out.Devices = map[string]*DevProfile{}
	}
	return &out
}

// ExeDir は設定ファイルの置き場所を決める。
func ConfigPath(exeDir string) string { return filepath.Join(exeDir, "shotpanel.json") }
