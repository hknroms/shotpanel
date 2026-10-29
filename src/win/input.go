//go:build windows

package win

import (
	"fmt"
	"hash/crc32"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Device はつながっているコントローラー1台。
type Device struct {
	Key      string            `json:"key"`
	Name     string            `json:"name"`
	Kind     string            `json:"kind"` // hid / xinput
	VID      uint16            `json:"vid"`
	PID      uint16            `json:"pid"`
	Conn     string            `json:"conn"` // USB / Bluetooth
	NButtons int               `json:"nButtons"`
	Controls []string          `json:"controls"` // この機器にある入力ID
	Presses  map[string]uint32 `json:"presses"`  // 押した回数の累計（手連射の計測用）

	state   map[string]bool
	stickLR float64
	stickUD float64
	hatDir  [4]bool

	// HID
	handle    uintptr
	preparsed []byte
	usageMax  uint32
	hat       *valCap
	axX, axY  *valCap
	xiIndex   int
}

type valCap struct {
	usage    uint16
	min, max int32
	bits     uint16
}

// Input はRaw InputとXInputをまとめて扱う。
type Input struct {
	mu       sync.Mutex
	devs     map[uintptr]*Device // HIDのハンドル → 機器
	ignored  map[uintptr]bool    // XInputの機器など、HIDとしては読まないもの
	xi       [4]*Device
	xiNames  map[uintptr]string
	OnChange func() // 接続・切断の通知
	hwnd     uintptr
}

func NewInput() *Input {
	return &Input{devs: map[uintptr]*Device{}, xiNames: map[uintptr]string{}, ignored: map[uintptr]bool{}}
}

// Snapshot は今の機器一覧と押されている入力を返す（スティックはレバー扱いの有無を反映）。
type DevState struct {
	*Device
	Pressed map[string]bool `json:"-"`
	Down    []string        `json:"down"`
}

func (in *Input) Snapshot(stickAsLever func(key string) bool) []DevState {
	in.mu.Lock()
	defer in.mu.Unlock()
	var out []DevState
	add := func(d *Device) {
		p := map[string]bool{}
		for k, v := range d.state {
			if v {
				p[k] = true
			}
		}
		dirs := [4]string{"up", "down", "left", "right"}
		for i, v := range d.hatDir {
			if v {
				p[dirs[i]] = true
			}
		}
		if stickAsLever(d.Key) {
			const th = 0.5
			if d.stickUD < -th {
				p["up"] = true
			}
			if d.stickUD > th {
				p["down"] = true
			}
			if d.stickLR < -th {
				p["left"] = true
			}
			if d.stickLR > th {
				p["right"] = true
			}
		}
		var down []string
		for k := range p {
			down = append(down, k)
		}
		sort.Strings(down)
		cp := *d
		cp.Presses = make(map[string]uint32, len(d.Presses))
		for k, v := range d.Presses {
			cp.Presses[k] = v
		}
		out = append(out, DevState{Device: &cp, Pressed: p, Down: down})
	}
	for _, d := range in.devs {
		if d.Key != "" {
			add(d)
		}
	}
	for _, d := range in.xi {
		if d != nil {
			add(d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Start はRaw Input用のウィンドウを作って受信を始める（専用スレッド）。
func (in *Input) Start(onReady func(hwnd uintptr)) {
	go func() {
		runtime.LockOSThread()
		h, err := newWindow("ShotPanelInput", in.wndProc)
		if err != nil {
			MessageBox("連射パネル設定", "入力用ウィンドウを作れませんでした: "+err.Error())
			return
		}
		in.hwnd = h
		type rid struct {
			page, usage uint16
			flags       uint32
			target      uintptr
		}
		const inputSink, devNotify = 0x100, 0x2000
		rids := []rid{{1, 4, inputSink | devNotify, h}, {1, 5, inputSink | devNotify, h}}
		pRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rids[0])), uintptr(len(rids)), unsafe.Sizeof(rids[0]))
		in.scanExisting()
		if onReady != nil {
			onReady(h)
		}
		messageLoop()
	}()
}

var extraProc func(hwnd uintptr, m uint32, w, l uintptr) (uintptr, bool)

func (in *Input) wndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmInput:
		in.onInput(l)
		return defProc(hwnd, m, w, l)
	case wmInputDeviceChange:
		if w == 1 {
			in.addHID(l)
		} else if w == 2 {
			in.mu.Lock()
			delete(in.devs, l)
			delete(in.ignored, l)
			delete(in.xiNames, l)
			in.mu.Unlock()
		}
		if in.OnChange != nil {
			go in.OnChange()
		}
		return 0
	}
	if extraProc != nil {
		if r, ok := extraProc(hwnd, m, w, l); ok {
			return r
		}
	}
	return defProc(hwnd, m, w, l)
}

func (in *Input) scanExisting() {
	var n uint32
	pGetRawInputDeviceList.Call(0, uintptr(unsafe.Pointer(&n)), 16)
	if n == 0 {
		return
	}
	list := make([]struct {
		h   uintptr
		typ uint32
		_   uint32
	}, n)
	pGetRawInputDeviceList.Call(uintptr(unsafe.Pointer(&list[0])), uintptr(unsafe.Pointer(&n)), 16)
	for _, d := range list[:n] {
		if d.typ == 2 {
			in.addHID(d.h)
		}
	}
}

func deviceName(h uintptr) string {
	var n uint32
	pGetRawInputDeviceInfoW.Call(h, 0x20000007, 0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	pGetRawInputDeviceInfoW.Call(h, 0x20000007, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	return windows.UTF16ToString(buf)
}

func hidStrings(path string) (product, serial string) {
	hf, err := windows.CreateFile(u16(path), 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return "", ""
	}
	defer windows.CloseHandle(hf)
	buf := make([]uint16, 128)
	if r, _, _ := pHidD_GetProductString.Call(uintptr(hf), uintptr(unsafe.Pointer(&buf[0])), 256); r != 0 {
		product = windows.UTF16ToString(buf)
	}
	buf2 := make([]uint16, 128)
	if r, _, _ := pHidD_GetSerialNumberString.Call(uintptr(hf), uintptr(unsafe.Pointer(&buf2[0])), 256); r != 0 {
		serial = windows.UTF16ToString(buf2)
	}
	return
}

const hidpOK = 0x00110000

func (in *Input) addHID(h uintptr) {
	path := deviceName(h)
	up := strings.ToUpper(path)
	// 機器の情報
	var info [8]uint32
	info[0] = 32
	sz := uint32(32)
	pGetRawInputDeviceInfoW.Call(h, 0x2000000b, uintptr(unsafe.Pointer(&info[0])), uintptr(unsafe.Pointer(&sz)))
	if info[1] != 2 {
		return
	}
	vid, pid := uint16(info[2]), uint16(info[3])
	usagePage, usage := uint16(info[5]), uint16(info[5]>>16)
	if usagePage != 1 || (usage != 4 && usage != 5) {
		return
	}
	product, serial := hidStrings(path)
	if strings.Contains(up, "IG_") {
		// XInputの機器はXInputで読む。名前だけ使う。
		in.mu.Lock()
		in.xiNames[h] = product
		in.mu.Unlock()
		return
	}
	// 解析用データ
	var n uint32
	pGetRawInputDeviceInfoW.Call(h, 0x20000005, 0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return
	}
	pre := make([]byte, n)
	pGetRawInputDeviceInfoW.Call(h, 0x20000005, uintptr(unsafe.Pointer(&pre[0])), uintptr(unsafe.Pointer(&n)))
	d := &Device{Kind: "hid", VID: vid, PID: pid, handle: h, preparsed: pre, state: map[string]bool{}}
	var caps [32]uint16
	if r, _, _ := pHidP_GetCaps.Call(uintptr(unsafe.Pointer(&pre[0])), uintptr(unsafe.Pointer(&caps[0]))); r != hidpOK {
		return
	}
	nb, nv := caps[23], caps[24]
	if nb > 0 {
		bc := make([]byte, 72*int(nb))
		l := nb
		pHidP_GetButtonCaps.Call(0, uintptr(unsafe.Pointer(&bc[0])), uintptr(unsafe.Pointer(&l)), uintptr(unsafe.Pointer(&pre[0])))
		for i := 0; i < int(l); i++ {
			c := bc[i*72:]
			if le16(c[0:]) != 9 {
				continue
			}
			mx := uint32(le16(c[56:]))
			if c[12] != 0 {
				mx = uint32(le16(c[58:]))
			}
			if mx > d.usageMax {
				d.usageMax = mx
			}
		}
	}
	if nv > 0 {
		vc := make([]byte, 72*int(nv))
		l := nv
		pHidP_GetValueCaps.Call(0, uintptr(unsafe.Pointer(&vc[0])), uintptr(unsafe.Pointer(&l)), uintptr(unsafe.Pointer(&pre[0])))
		for i := 0; i < int(l); i++ {
			c := vc[i*72:]
			if le16(c[0:]) != 1 {
				continue
			}
			v := &valCap{usage: le16(c[56:]), bits: le16(c[18:]), min: int32(le32(c[40:])), max: int32(le32(c[44:]))}
			if c[12] != 0 {
				continue
			}
			switch v.usage {
			case 0x39:
				d.hat = v
			case 0x30:
				d.axX = v
			case 0x31:
				d.axY = v
			}
		}
	}
	if d.usageMax > 32 {
		d.usageMax = 32
	}
	d.NButtons = int(d.usageMax)
	for i := 1; i <= d.NButtons; i++ {
		d.Controls = append(d.Controls, fmt.Sprintf("btn%d", i))
	}
	d.Controls = append(d.Controls, "up", "down", "left", "right")
	if product == "" {
		product = fmt.Sprintf("コントローラー %04X:%04X", vid, pid)
	}
	d.Name = product
	d.Conn = "USB"
	if strings.Contains(up, "{00001124-") || strings.Contains(up, "BTHENUM") || strings.Contains(up, "_PID&") || strings.Contains(up, "00001812") {
		d.Conn = "Bluetooth"
	}
	id := serial
	if id == "" {
		id = fmt.Sprintf("p%08x", crc32.ChecksumIEEE([]byte(up)))
	}
	d.Key = fmt.Sprintf("hid:%04x:%04x:%s", vid, pid, id)
	in.mu.Lock()
	in.devs[h] = d
	in.mu.Unlock()
}

func le16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }
func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

var rawBuf = make([]byte, 4096)

func (in *Input) onInput(l uintptr) {
	sz := uint32(len(rawBuf))
	r, _, _ := pGetRawInputData.Call(l, 0x10000003, uintptr(unsafe.Pointer(&rawBuf[0])), uintptr(unsafe.Pointer(&sz)), 24)
	if int32(r) <= 0 {
		return
	}
	h := *(*uintptr)(unsafe.Pointer(&rawBuf[8]))
	in.mu.Lock()
	d := in.devs[h]
	in.mu.Unlock()
	if d == nil {
		in.mu.Lock()
		ign := in.ignored[h]
		in.mu.Unlock()
		if ign {
			return
		}
		in.addHID(h)
		in.mu.Lock()
		d = in.devs[h]
		in.mu.Unlock()
		if d == nil {
			in.mu.Lock()
			in.ignored[h] = true
			in.mu.Unlock()
			return
		}
		if in.OnChange != nil {
			go in.OnChange()
		}
	}
	size := le32(rawBuf[24:])
	count := le32(rawBuf[28:])
	if size == 0 || count == 0 || 32+int(size*count) > int(r) {
		return
	}
	for i := uint32(0); i < count; i++ {
		rep := rawBuf[32+i*size : 32+(i+1)*size]
		in.parse(d, rep)
	}
}

func (in *Input) parse(d *Device, rep []byte) {
	pre := uintptr(unsafe.Pointer(&d.preparsed[0]))
	ml, _, _ := pHidP_MaxUsageListLength.Call(0, 9, pre)
	if ml == 0 {
		ml = 1
	}
	usages := make([]uint16, ml)
	n := uint32(ml)
	r, _, _ := pHidP_GetUsages.Call(0, 9, 0, uintptr(unsafe.Pointer(&usages[0])), uintptr(unsafe.Pointer(&n)), pre, uintptr(unsafe.Pointer(&rep[0])), uintptr(len(rep)))
	if r != hidpOK {
		return // 別のレポート（振動・ジャイロなど）は無視
	}
	st := map[string]bool{}
	for _, u := range usages[:n] {
		if u >= 1 && u <= 32 {
			st[fmt.Sprintf("btn%d", u)] = true
		}
	}
	get := func(v *valCap) (float64, bool) {
		if v == nil {
			return 0, false
		}
		var raw uint32
		r, _, _ := pHidP_GetUsageValue.Call(0, 1, 0, uintptr(v.usage), uintptr(unsafe.Pointer(&raw)), pre, uintptr(unsafe.Pointer(&rep[0])), uintptr(len(rep)))
		if r != hidpOK {
			return 0, false
		}
		val := int64(raw)
		if v.min < 0 && v.bits > 0 && v.bits < 32 && raw&(1<<(v.bits-1)) != 0 {
			val = int64(raw) - (1 << v.bits)
		}
		return float64(val), true
	}
	var hat [4]bool
	if v, ok := get(d.hat); ok {
		mn, mx := float64(d.hat.min), float64(d.hat.max)
		if v >= mn && v <= mx {
			steps := mx - mn + 1
			ang := (v - mn) * 360 / steps
			rad := ang * math.Pi / 180
			x, y := math.Sin(rad), -math.Cos(rad)
			hat = [4]bool{y < -0.3, y > 0.3, x < -0.3, x > 0.3}
		}
	}
	norm := func(v *valCap) float64 {
		f, ok := get(v)
		if !ok || v.max == v.min {
			return 0
		}
		mn, mx := float64(v.min), float64(v.max)
		if v.max < v.min { // 符号の扱いが壊れている機器
			mn, mx = 0, float64(uint32(1)<<v.bits-1)
		}
		return (f-mn)/(mx-mn)*2 - 1
	}
	lr, ud := norm(d.axX), norm(d.axY)
	in.mu.Lock()
	countPresses(d, st)
	d.state = st
	d.hatDir = hat
	d.stickLR, d.stickUD = lr, ud
	in.mu.Unlock()
}

// countPresses は新しく押された入力を数える（mu をロック中に呼ぶ）。
func countPresses(d *Device, st map[string]bool) {
	if d.Presses == nil {
		d.Presses = map[string]uint32{}
	}
	for k, v := range st {
		if v && !d.state[k] {
			d.Presses[k]++
		}
	}
}
