//go:build windows

package win

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	wmTray = wmApp + 1
	WMShow = wmApp + 2
)

// Tray は通知領域アイコン。
type Tray struct {
	hwnd    uintptr
	data    [976]byte
	OnOpen  func()
	OnPause func()
	OnQuit  func()
	Paused  func() bool
}

func appIcon() uintptr {
	// exe に埋め込んだアイコン（リソースID 2。1はマニフェスト）、無ければ標準アイコン
	if h, _, _ := pLoadIconW.Call(moduleHandle(), 2); h != 0 {
		return h
	}
	h, _, _ := pLoadIconW.Call(0, 32512)
	return h
}

// Add は入力用ウィンドウにアイコンを付ける（入力スレッドから呼ぶ）。
func (t *Tray) Add(hwnd uintptr, tip string) {
	t.hwnd = hwnd
	d := &t.data
	*(*uint32)(unsafe.Pointer(&d[0])) = uint32(len(d))
	*(*uintptr)(unsafe.Pointer(&d[8])) = hwnd
	*(*uint32)(unsafe.Pointer(&d[16])) = 1
	*(*uint32)(unsafe.Pointer(&d[20])) = 1 | 2 | 4
	*(*uint32)(unsafe.Pointer(&d[24])) = wmTray
	*(*uintptr)(unsafe.Pointer(&d[32])) = appIcon()
	t.SetTip(tip)
	pShell_NotifyIconW.Call(0, uintptr(unsafe.Pointer(&d[0])))
	extraProc = t.proc
}

// SetTip はマウスを乗せたときの文字を変える。
func (t *Tray) SetTip(tip string) {
	u, _ := windows.UTF16FromString(tip)
	if len(u) > 127 {
		u = append(u[:126], 0)
	}
	for i := 0; i < 128; i++ {
		var c uint16
		if i < len(u) {
			c = u[i]
		}
		*(*uint16)(unsafe.Pointer(&t.data[40+i*2])) = c
	}
	if t.hwnd != 0 {
		pShell_NotifyIconW.Call(1, uintptr(unsafe.Pointer(&t.data[0])))
	}
}

func (t *Tray) Remove() { pShell_NotifyIconW.Call(2, uintptr(unsafe.Pointer(&t.data[0]))) }

func (t *Tray) proc(hwnd uintptr, m uint32, w, l uintptr) (uintptr, bool) {
	switch m {
	case wmTray:
		switch uint32(l) & 0xFFFF {
		case wmLButtonDblClk, wmLButtonUp:
			if t.OnOpen != nil {
				go t.OnOpen()
			}
		case wmRButtonUp:
			t.menu(hwnd)
		}
		return 0, true
	case WMShow:
		if t.OnOpen != nil {
			go t.OnOpen()
		}
		return 0, true
	}
	return 0, false
}

func (t *Tray) menu(hwnd uintptr) {
	m, _, _ := pCreatePopupMenu.Call()
	pause := "一時停止"
	if t.Paused != nil && t.Paused() {
		pause = "再開"
	}
	pAppendMenuW.Call(m, 0, 1, uintptr(unsafe.Pointer(u16("設定を開く"))))
	pAppendMenuW.Call(m, 0, 2, uintptr(unsafe.Pointer(u16(pause))))
	pAppendMenuW.Call(m, 0x800, 0, 0)
	pAppendMenuW.Call(m, 0, 3, uintptr(unsafe.Pointer(u16("終了"))))
	var pt struct{ X, Y int32 }
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, 0x100|0x2, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pPostMessageW.Call(hwnd, 0, 0, 0)
	pDestroyMenu.Call(m)
	switch cmd {
	case 1:
		if t.OnOpen != nil {
			go t.OnOpen()
		}
	case 2:
		if t.OnPause != nil {
			go t.OnPause()
		}
	case 3:
		if t.OnQuit != nil {
			go t.OnQuit()
		}
	}
}

var (
	origProc  uintptr
	closeHook func()
	hookCB    = syscall.NewCallback(func(hwnd uintptr, m uint32, w, l uintptr) uintptr {
		if m == wmClose && closeHook != nil {
			closeHook()
			return 0
		}
		r, _, _ := pCallWindowProcW.Call(origProc, hwnd, uintptr(m), w, l)
		return r
	})
)

// HookClose は×ボタンで終了せず、onClose を呼ぶようにする。
func HookClose(hwnd uintptr, onClose func()) {
	closeHook = onClose
	r, _, _ := pSetWindowLongPtrW.Call(hwnd, ^uintptr(3), hookCB) // GWLP_WNDPROC = -4
	origProc = r
	icon := appIcon()
	pSendMessageW.Call(hwnd, wmSetIcon, 0, icon)
	pSendMessageW.Call(hwnd, wmSetIcon, 1, icon)
}

// ShowWindow は画面を出す・隠す。
func ShowWindow(hwnd uintptr, show bool) {
	if show {
		pShowWindow.Call(hwnd, 9) // SW_RESTORE
		pShowWindow.Call(hwnd, 5)
		pSetForegroundWindow.Call(hwnd)
	} else {
		pShowWindow.Call(hwnd, 0)
	}
}

func IsVisible(hwnd uintptr) bool { r, _, _ := pIsWindowVisibleP.Call(hwnd); return r != 0 }

// SingleInstance は2つ目の起動なら、先に起動している方に「画面を出して」と送って false を返す。
func SingleInstance() bool {
	pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(u16("ShotPanel-SingleInstance"))))
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16("ShotPanelInput"))), 0); h != 0 {
			pPostMessageW.Call(h, WMShow, 0, 0)
		}
		return false
	}
	return true
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Autostart はWindows起動時に常駐させるかどうか。
func Autostart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("ShotPanel")
	return err == nil
}

func SetAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		err := k.DeleteValue("ShotPanel")
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	exe, _ := os.Executable()
	return k.SetStringValue("ShotPanel", `"`+exe+`" /tray`)
}
