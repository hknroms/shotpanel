package core

import (
	"math"
	"strings"
	"time"
)

// 連射速度（発/秒）→ 押すフレーム数・離すフレーム数（60フレーム基準）
var speedFrames = map[float64][2]int{
	30: {1, 1}, 20: {1, 2}, 15: {2, 2}, 12: {2, 3}, 10: {3, 3},
	8.6: {3, 4}, 7.5: {4, 4}, 5: {6, 6}, 3: {10, 10},
}

// Frames は最も近い速度のフレーム数を返す。
func Frames(speed float64) (on, off int) {
	best, bd := [2]int{2, 2}, math.MaxFloat64
	for s, f := range speedFrames {
		if d := math.Abs(s - speed); d < bd {
			bd, best = d, f
		}
	}
	return best[0], best[1]
}

const frame = time.Second / 60

// PatternOf は設定から押す・離すフレーム数を決める（on/off の指定が優先）。
func PatternOf(speed float64, on, off int) (int, int) {
	if on > 0 && off > 0 {
		return clampF(on), clampF(off)
	}
	return Frames(speed)
}

func clampF(n int) int {
	if n > 30 {
		return 30
	}
	return n
}

// TurboOn は押し始めからの経過時間で連射の押し/離しを決める。押した瞬間は必ず「押し」。
func TurboOn(elapsed time.Duration, speed float64) bool {
	on, off := Frames(speed)
	return TurboOnF(elapsed, on, off)
}

func TurboOnF(elapsed time.Duration, on, off int) bool {
	f := int(elapsed / frame)
	return f%(on+off) < on
}

// DevInput はエンジンへの1台分の入力。
type DevInput struct {
	Key      string
	Assign   int               // 1/2/0
	Controls map[string]bool   // 物理入力ID → 押されているか
	Map      map[string]string // 位置 → 物理入力ID
	Prof     *DevProfile
}

// Engine は押しっぱなしの開始時刻を覚えて、出すべきキーを計算する。
type Engine struct {
	since map[string]time.Time
}

func NewEngine() *Engine { return &Engine{since: map[string]time.Time{}} }

// Output は「どのプレイヤーのどの機能を押すか」の集合（"1:B1" など）を返す。
func (e *Engine) Output(now time.Time, devs []DevInput) map[string]bool {
	out := map[string]bool{}
	seen := map[string]bool{}
	held := func(id string, pressed bool) (time.Duration, bool) {
		if !pressed {
			return 0, false
		}
		seen[id] = true
		t, ok := e.since[id]
		if !ok {
			t = now
			e.since[id] = now
		}
		return now.Sub(t), true
	}
	for _, d := range devs {
		if d.Assign != 1 && d.Assign != 2 || d.Prof == nil {
			continue
		}
		pl := d.Assign
		other := 3 - pl
		emit := func(id string, c ButtonCfg, pressed bool) {
			if c.Mode == "off" || c.Mode == "" {
				return
			}
			el, p := held(id, pressed)
			if !p {
				return
			}
			if on, off := PatternOf(c.Speed, c.On, c.Off); c.Mode == "turbo" && !TurboOnF(el, on, off) {
				return
			}
			op := pl
			if c.Other {
				op = other
			}
			for _, t := range strings.Split(c.Target, "+") {
				if t != "" {
					out[itoa(op)+":"+t] = true
				}
			}
		}
		for pos, ctl := range d.Map {
			if c, ok := d.Prof.Cfg[pos]; ok && ctl != "" {
				emit(d.Key+"/"+pos, c, d.Controls[ctl])
			}
		}
		// レバー（方向ごとの割り当て "dir_up" などが無ければ、そのまま上下左右）
		dirs := []string{"UP", "DOWN", "LEFT", "RIGHT"}
		alt := d.Prof.Alt
		altAxisLR := alt.Axis != "上下"
		for _, dn := range dirs {
			lo := strings.ToLower(dn)
			isLR := dn == "LEFT" || dn == "RIGHT"
			if alt.On && isLR == altAxisLR {
				continue // 交互連打は下で処理
			}
			c, ok := d.Prof.Cfg["dir_"+lo]
			if !ok {
				c = ButtonCfg{Mode: "normal", Target: dn}
			}
			emit(d.Key+"/dir_"+lo, c, d.Controls[lo])
		}
		if alt.On {
			a, b := "left", "right"
			if !altAxisLR {
				a, b = "up", "down"
			}
			el, p := held(d.Key+"/alt", d.Controls[a] || d.Controls[b])
			if p {
				on, off := PatternOf(alt.Speed, alt.POn, alt.POff)
				f := int(el/frame) / (on + off) // 1周期ごとに方向を入れ替える
				dir := a
				if f%2 == 1 {
					dir = b
				}
				if TurboOnF(el, on, off) {
					out[itoa(pl)+":"+strings.ToUpper(dir)] = true
				}
			}
		}
	}
	for k := range e.since {
		if !seen[k] {
			delete(e.since, k)
		}
	}
	return out
}

func itoa(i int) string {
	if i == 2 {
		return "2"
	}
	return "1"
}
