package core

import (
	"math"
	"sort"
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

// Req は「押されている1つの入力」が出したい出力。連射のパターンと行き先を持つ。
// MAMEのプラグインには、これをそのまま渡してフレームに合わせて押し離しさせる。
type Req struct {
	N     uint64    // 押し始めごとに変わる番号（プラグインはこれが変わったら1フレーム目から数え直す）
	Start time.Time // 押し始めた時刻
	On    int       // 押すフレーム数
	Off   int       // 離すフレーム数（0 = 押しっぱなし）
	A     []string  // 行き先（"1:B1" "2:UP" "1:SYS:SERVICE1" など）
	B     []string  // 交互連打のときの、1周期おきの行き先（それ以外は空）
}

type press struct {
	t time.Time
	n uint64
}

// Engine は押しっぱなしの開始時刻を覚えて、出すべき出力を計算する。
type Engine struct {
	since map[string]press
	next  uint64
}

func NewEngine() *Engine { return &Engine{since: map[string]press{}} }

// Output は「どのプレイヤーのどの機能を押すか」の集合（"1:B1" など）を返す（PCの時計で連射する）。
func (e *Engine) Output(now time.Time, devs []DevInput) map[string]bool {
	return EvalReqs(now, e.Requests(now, devs))
}

// EvalReqs は PC の時計（1F=1/60秒）で、いま押すべき出力を決める。
func EvalReqs(now time.Time, reqs []Req) map[string]bool {
	out := map[string]bool{}
	for _, r := range reqs {
		el := now.Sub(r.Start)
		if r.Off > 0 && !TurboOnF(el, r.On, r.Off) {
			continue
		}
		t := r.A
		if len(r.B) > 0 && (int(el/frame)/(r.On+r.Off))%2 == 1 {
			t = r.B
		}
		for _, x := range t {
			out[x] = true
		}
	}
	return out
}

// Requests は押されている入力ごとの出力（連射のパターン付き）を返す。
func (e *Engine) Requests(now time.Time, devs []DevInput) []Req {
	var reqs []Req
	seen := map[string]bool{}
	held := func(id string, pressed bool) (press, bool) {
		if !pressed {
			return press{}, false
		}
		seen[id] = true
		p, ok := e.since[id]
		if !ok {
			e.next++
			p = press{now, e.next}
			e.since[id] = p
		}
		return p, true
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
			p, ok := held(id, pressed)
			if !ok {
				return
			}
			r := Req{N: p.n, Start: p.t, On: 1}
			if c.Mode == "turbo" {
				r.On, r.Off = PatternOf(c.Speed, c.On, c.Off)
			}
			op := pl
			if c.Other {
				op = other
			}
			for _, t := range strings.Split(c.Target, "+") {
				if t != "" {
					r.A = append(r.A, itoa(op)+":"+t)
				}
			}
			if len(r.A) > 0 {
				reqs = append(reqs, r)
			}
		}
		// 位置の順に並べる（毎回同じ順にするため）
		poss := make([]string, 0, len(d.Map))
		for pos := range d.Map {
			poss = append(poss, pos)
		}
		sort.Strings(poss)
		for _, pos := range poss {
			ctl := d.Map[pos]
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
			if p, ok := held(d.Key+"/alt", d.Controls[a] || d.Controls[b]); ok {
				on, off := PatternOf(alt.Speed, alt.POn, alt.POff)
				reqs = append(reqs, Req{N: p.n, Start: p.t, On: on, Off: off,
					A: []string{itoa(pl) + ":" + strings.ToUpper(a)}, B: []string{itoa(pl) + ":" + strings.ToUpper(b)}})
			}
		}
	}
	for k := range e.since {
		if !seen[k] {
			delete(e.since, k)
		}
	}
	return reqs
}

func itoa(i int) string {
	if i == 2 {
		return "2"
	}
	return "1"
}
