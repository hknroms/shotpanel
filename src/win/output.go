//go:build windows

package win

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func syscallN(p uintptr, args ...uintptr) (uintptr, uintptr, error) {
	r1, r2, e := syscall.SyscallN(p, args...)
	if e != 0 {
		return r1, r2, e
	}
	return r1, r2, nil
}

// MAMEのキー名 → スキャンコード（0x100 は拡張キー）
var scan = map[string]uint16{
	"ESC": 0x01, "1": 0x02, "2": 0x03, "3": 0x04, "4": 0x05, "5": 0x06, "6": 0x07, "7": 0x08, "8": 0x09, "9": 0x0A, "0": 0x0B,
	"MINUS": 0x0C, "EQUALS": 0x0D, "BACKSPACE": 0x0E, "TAB": 0x0F,
	"Q": 0x10, "W": 0x11, "E": 0x12, "R": 0x13, "T": 0x14, "Y": 0x15, "U": 0x16, "I": 0x17, "O": 0x18, "P": 0x19,
	"OPENBRACE": 0x1A, "CLOSEBRACE": 0x1B, "ENTER": 0x1C, "LCONTROL": 0x1D,
	"A": 0x1E, "S": 0x1F, "D": 0x20, "F": 0x21, "G": 0x22, "H": 0x23, "J": 0x24, "K": 0x25, "L": 0x26,
	"COLON": 0x27, "QUOTE": 0x28, "TILDE": 0x29, "LSHIFT": 0x2A, "BACKSLASH": 0x2B,
	"Z": 0x2C, "X": 0x2D, "C": 0x2E, "V": 0x2F, "B": 0x30, "N": 0x31, "M": 0x32,
	"COMMA": 0x33, "STOP": 0x34, "SLASH": 0x35, "RSHIFT": 0x36, "ASTERISK": 0x37, "LALT": 0x38, "SPACE": 0x39,
	"F1": 0x3B, "F2": 0x3C, "F3": 0x3D, "F4": 0x3E, "F5": 0x3F, "F6": 0x40, "F7": 0x41, "F8": 0x42, "F9": 0x43, "F10": 0x44,
	"7_PAD": 0x47, "8_PAD": 0x48, "9_PAD": 0x49, "MINUS_PAD": 0x4A, "4_PAD": 0x4B, "5_PAD": 0x4C, "6_PAD": 0x4D,
	"PLUS_PAD": 0x4E, "1_PAD": 0x4F, "2_PAD": 0x50, "3_PAD": 0x51, "0_PAD": 0x52, "DEL_PAD": 0x53,
	"F11": 0x57, "F12": 0x58,
	"ENTER_PAD": 0x11C, "RCONTROL": 0x11D, "SLASH_PAD": 0x135, "RALT": 0x138,
	"HOME": 0x147, "UP": 0x148, "PGUP": 0x149, "LEFT": 0x14B, "RIGHT": 0x14D, "END": 0x14F, "DOWN": 0x150,
	"PGDN": 0x151, "INSERT": 0x152, "DEL": 0x153,
}

// KeyNames は設定画面で選べるキー名。
func KeyNames() []string {
	var out []string
	for k := range scan {
		out = append(out, k)
	}
	return out
}

// SendKey はキーを押す・離す（スキャンコードで送るのでMAMEのDirectInputでも読める）。
func SendKey(name string, down bool) {
	sc, ok := scan[name]
	if !ok {
		return
	}
	var in [40]byte
	*(*uint32)(unsafe.Pointer(&in[0])) = 1 // INPUT_KEYBOARD
	*(*uint16)(unsafe.Pointer(&in[10])) = sc & 0xFF
	flags := uint32(0x0008) // SCANCODE
	if sc&0x100 != 0 {
		flags |= 0x0001
	}
	if !down {
		flags |= 0x0002
	}
	*(*uint32)(unsafe.Pointer(&in[12])) = flags
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in[0])), 40)
}

// MameWindow は見つかったMAMEのゲーム画面。
type MameWindow struct {
	Hwnd  uintptr
	Title string
	Exe   string
	PID   uint32
}

var (
	findParse func(title string, exeIsMame bool) bool
	findOut   *MameWindow
	findCB    = syscall.NewCallback(func(h uintptr, _ uintptr) uintptr {
		if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
			return 1
		}
		var buf [512]uint16
		n, _, _ := pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 512)
		if n == 0 {
			return 1
		}
		title := windows.UTF16ToString(buf[:n])
		if !strings.Contains(title, "[") {
			return 1
		}
		var pid uint32
		pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		exe := processPath(pid)
		isMame := strings.Contains(strings.ToLower(filepath.Base(exe)), "mame")
		if findParse(title, isMame) {
			findOut = &MameWindow{Hwnd: h, Title: title, Exe: exe, PID: pid}
			return 0
		}
		return 1
	})
)

// FindMame は見えているトップレベルウィンドウからMAMEのゲーム画面を探す（1つのゴルーチンからだけ呼ぶ）。
func FindMame(parse func(title string, exeIsMame bool) bool) *MameWindow {
	findParse, findOut = parse, nil
	pEnumWindows.Call(findCB, 0)
	return findOut
}

func processPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := pQueryFullProcessImageNameW.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// ForegroundPID は前面のウィンドウのプロセスID。
func ForegroundPID() uint32 {
	h, _, _ := pGetForegroundWindow.Call()
	if h == 0 {
		return 0
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	return pid
}

var (
	anyMame   bool
	anyMameCB = syscall.NewCallback(func(h uintptr, _ uintptr) uintptr {
		if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
			return 1
		}
		var pid uint32
		pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		if strings.Contains(strings.ToLower(filepath.Base(processPath(pid))), "mame") {
			anyMame = true
			return 0
		}
		return 1
	})
)

// MameAppOpen はMAMEやMAMEUIの画面（ゲームを起動していない MAMEUI も含む）が開いているか。
func MameAppOpen() bool {
	anyMame = false
	pEnumWindows.Call(anyMameCB, 0)
	return anyMame
}
