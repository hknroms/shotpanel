//go:build windows

package win

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	xiGetState uintptr
	xiChecked  bool
)

func loadXInput() {
	xiChecked = true
	for _, name := range []string{"xinput1_4.dll", "xinput1_3.dll", "xinput9_1_0.dll"} {
		h, err := windows.LoadLibraryEx(name, 0, windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
		if err != nil {
			continue
		}
		// 序数100 = XInputGetStateEx（ガイドボタンも読める）
		if p, err := windows.GetProcAddressByOrdinal(h, 100); err == nil && name != "xinput9_1_0.dll" {
			xiGetState = p
			return
		}
		if p, err := windows.GetProcAddress(h, "XInputGetState"); err == nil {
			xiGetState = p
			return
		}
	}
}

type xiState struct {
	Packet  uint32
	Buttons uint16
	LT, RT  uint8
	LX, LY  int16
	RX, RY  int16
	_       [4]uint32 // XInputGetStateEx 用の余白
}

var xiButtons = []struct {
	bit uint16
	id  string
}{
	{0x0010, "START"}, {0x0020, "BACK"}, {0x0040, "LS"}, {0x0080, "RS"},
	{0x0100, "LB"}, {0x0200, "RB"}, {0x0400, "GUIDE"},
	{0x1000, "A"}, {0x2000, "B"}, {0x4000, "X"}, {0x8000, "Y"},
}

var xiControls = []string{"A", "B", "X", "Y", "LB", "RB", "LT", "RT", "BACK", "START", "LS", "RS", "GUIDE", "up", "down", "left", "right"}

var xiNextProbe [4]time.Time

// PollXInput はXInputの4台分を読む。つながっていない枠は1秒ごとにだけ確認する。
func (in *Input) PollXInput(now time.Time) (changed bool) {
	if !xiChecked {
		loadXInput()
	}
	if xiGetState == 0 {
		return false
	}
	for i := 0; i < 4; i++ {
		in.mu.Lock()
		d := in.xi[i]
		in.mu.Unlock()
		if d == nil && now.Before(xiNextProbe[i]) {
			continue
		}
		var s xiState
		r, _, _ := syscallN(xiGetState, uintptr(i), uintptr(unsafe.Pointer(&s)))
		if r != 0 {
			if d != nil {
				in.mu.Lock()
				in.xi[i] = nil
				in.mu.Unlock()
				changed = true
			}
			xiNextProbe[i] = now.Add(time.Second)
			continue
		}
		if d == nil {
			name := ""
			in.mu.Lock()
			if len(in.xiNames) == 1 {
				for _, n := range in.xiNames {
					name = n
				}
			}
			in.mu.Unlock()
			if name == "" {
				name = "Xbox互換コントローラー"
			}
			d = &Device{Key: fmt.Sprintf("xinput:%d", i), Name: fmt.Sprintf("%s（%d）", name, i+1), Kind: "xinput", Conn: "USB",
				Controls: xiControls, NButtons: 13, state: map[string]bool{}, xiIndex: i}
			in.mu.Lock()
			in.xi[i] = d
			in.mu.Unlock()
			changed = true
		}
		st := map[string]bool{}
		for _, b := range xiButtons {
			if s.Buttons&b.bit != 0 {
				st[b.id] = true
			}
		}
		if s.LT > 30 {
			st["LT"] = true
		}
		if s.RT > 30 {
			st["RT"] = true
		}
		in.mu.Lock()
		countPresses(d, st)
		d.state = st
		d.hatDir = [4]bool{s.Buttons&1 != 0, s.Buttons&2 != 0, s.Buttons&4 != 0, s.Buttons&8 != 0}
		d.stickLR = float64(s.LX) / 32768
		d.stickUD = -float64(s.LY) / 32768
		in.mu.Unlock()
	}
	return changed
}
