package core

import (
	"fmt"
	"strings"
)

// 物理入力IDの決まり
//   HIDの汎用ボタン: "btn1".."btn32"
//   XInput:          "A" "B" "X" "Y" "LB" "RB" "LT" "RT" "BACK" "START" "LS" "RS"
//   方向（ハット・十字キー・スティックを合成）: "up" "down" "left" "right"

// 位置の決まり（すべての配置で共通）
//   b1 b2 b3 b4 / b5 b6 b7 b8 = 上段・下段（PS系なら □△R1L1 / ×○R2L2）
//   start coin = スタート・コイン（PSならOPTIONS・SHARE）
//   l3 r3 ps touch mute xN = その他のボタン

const SonyVID = 0x054C

// DefaultMap はコントローラーの種類から、位置→物理入力の初期対応を作る。
func DefaultMap(kind string, vid uint16, nButtons int) map[string]string {
	m := map[string]string{}
	if kind == "xinput" {
		for k, v := range map[string]string{"b1": "X", "b2": "Y", "b3": "RB", "b4": "LB", "b5": "A", "b6": "B", "b7": "RT", "b8": "LT",
			"coin": "BACK", "start": "START", "l3": "LS", "r3": "RS", "ps": "GUIDE"} {
			m[k] = v
		}
		return m
	}
	// PS4/PS3系の汎用HIDの順番: 1□ 2× 3○ 4△ 5L1 6R1 7L2 8R2 9SHARE 10OPTIONS 11L3 12R3 13PS 14タッチパッド 15ミュート
	order := []string{"b1", "b5", "b6", "b2", "b4", "b3", "b8", "b7", "coin", "start", "l3", "r3", "ps", "touch", "mute"}
	for i := 0; i < nButtons; i++ {
		if i < len(order) {
			if order[i] == "mute" && vid != SonyVID {
				m[fmt.Sprintf("x%d", i+1)] = fmt.Sprintf("btn%d", i+1)
				continue
			}
			m[order[i]] = fmt.Sprintf("btn%d", i+1)
		} else {
			m[fmt.Sprintf("x%d", i+1)] = fmt.Sprintf("btn%d", i+1)
		}
	}
	return m
}

// DefaultPhys はボタン表記の初期値。
func DefaultPhys(kind string) string {
	if kind == "xinput" {
		return "xbox"
	}
	return "ps"
}

// DefaultLayout は、新しくつないだコントローラーの配置の初期値。
// 2026-10-30 ユーザー決定：パッド・アケコンを問わず「1レバー8ボタン＋スタート（コイン別）」（8b）にする。
// （以前は名前から推測し、XInput・パッドは "pad" にしていた）
func DefaultLayout(name string, kind string, vid uint16) string {
	return "8b"
}

// ExtraLabel はその他ボタンの表示名。
func ExtraLabel(pos string, vid uint16, kind string) string {
	switch pos {
	case "l3":
		return "L3"
	case "r3":
		return "R3"
	case "ps":
		if vid == SonyVID {
			return "PSボタン"
		}
		return "HOME"
	case "touch":
		if vid == SonyVID {
			return "タッチパッド"
		}
		return "ボタン14"
	case "mute":
		return "マイクミュート"
	}
	if strings.HasPrefix(pos, "x") {
		return "ボタン" + pos[1:]
	}
	return pos
}

// Slots6 は6ボタン配置パターンのA〜Fに当たる位置。
var Slots6 = []string{"b1", "b2", "b3", "b5", "b6", "b7"}

// DefaultProfile は設定がまだ無いコントローラーの初期値（A〜FをB1〜B6に単発）。
func DefaultProfile(layout string) *DevProfile {
	p := &DevProfile{Preset: "6b1", Cfg: map[string]ButtonCfg{}, Alt: AltCfg{Axis: "左右", Speed: 15}}
	slots := Slots6
	if layout == "3b" {
		slots = Slots6[:3]
		p.Preset = "3b3"
	}
	for i, k := range slots {
		p.Cfg[k] = ButtonCfg{Mode: "normal", Target: "B" + string(rune('1'+i)), Speed: 30}
	}
	p.Cfg["start"] = ButtonCfg{Mode: "normal", Target: "START", Speed: 30}
	p.Cfg["coin"] = ButtonCfg{Mode: "normal", Target: "COIN", Speed: 30}
	return p
}
