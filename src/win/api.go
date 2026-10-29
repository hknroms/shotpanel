//go:build windows

// Package win はWindowsのAPI（入力の読み取り・キー送信・ウィンドウ検出・常駐）。
package win

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	hid      = windows.NewLazySystemDLL("hid.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	winmm    = windows.NewLazySystemDLL("winmm.dll")

	pRegisterClassExW         = user32.NewProc("RegisterClassExW")
	pCreateWindowExW          = user32.NewProc("CreateWindowExW")
	pDefWindowProcW           = user32.NewProc("DefWindowProcW")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessageW         = user32.NewProc("DispatchMessageW")
	pPostMessageW             = user32.NewProc("PostMessageW")
	pPostQuitMessage          = user32.NewProc("PostQuitMessage")
	pRegisterRawInputDevices  = user32.NewProc("RegisterRawInputDevices")
	pGetRawInputData          = user32.NewProc("GetRawInputData")
	pGetRawInputDeviceInfoW   = user32.NewProc("GetRawInputDeviceInfoW")
	pGetRawInputDeviceList    = user32.NewProc("GetRawInputDeviceList")
	pSendInput                = user32.NewProc("SendInput")
	pEnumWindows              = user32.NewProc("EnumWindows")
	pGetWindowTextW           = user32.NewProc("GetWindowTextW")
	pIsWindowVisible          = user32.NewProc("IsWindowVisible")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	pGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pShowWindow               = user32.NewProc("ShowWindow")
	pIsWindowVisibleP         = user32.NewProc("IsWindowVisible")
	pLoadIconW                = user32.NewProc("LoadIconW")
	pCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	pAppendMenuW              = user32.NewProc("AppendMenuW")
	pTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	pDestroyMenu              = user32.NewProc("DestroyMenu")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	pCallWindowProcW          = user32.NewProc("CallWindowProcW")
	pFindWindowW              = user32.NewProc("FindWindowW")
	pMessageBoxW              = user32.NewProc("MessageBoxW")
	pSendMessageW             = user32.NewProc("SendMessageW")
	pLoadImageW               = user32.NewProc("LoadImageW")

	pGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	pQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	pCreateMutexW               = kernel32.NewProc("CreateMutexW")

	pHidP_GetCaps             = hid.NewProc("HidP_GetCaps")
	pHidP_GetButtonCaps       = hid.NewProc("HidP_GetButtonCaps")
	pHidP_GetValueCaps        = hid.NewProc("HidP_GetValueCaps")
	pHidP_GetUsages           = hid.NewProc("HidP_GetUsages")
	pHidP_GetUsageValue       = hid.NewProc("HidP_GetUsageValue")
	pHidP_MaxUsageListLength  = hid.NewProc("HidP_MaxUsageListLength")
	pHidD_GetProductString    = hid.NewProc("HidD_GetProductString")
	pHidD_GetSerialNumberString = hid.NewProc("HidD_GetSerialNumberString")

	pShell_NotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pTimeBeginPeriod   = winmm.NewProc("timeBeginPeriod")
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	_       uint32
}

const (
	wmDestroy           = 0x0002
	wmClose             = 0x0010
	wmCommand           = 0x0111
	wmInput             = 0x00FF
	wmInputDeviceChange = 0x00FE
	wmApp               = 0x8000
	wmLButtonDblClk     = 0x0203
	wmLButtonUp         = 0x0202
	wmRButtonUp         = 0x0205
	wmSetIcon           = 0x0080
)

func u16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func moduleHandle() uintptr { h, _, _ := pGetModuleHandleW.Call(0); return h }

// newWindow は見えないウィンドウを作る（Raw Input と通知領域アイコン用）。
func newWindow(class string, proc func(hwnd uintptr, m uint32, w, l uintptr) uintptr) (uintptr, error) {
	cb := syscall.NewCallback(func(hwnd uintptr, m uint32, w, l uintptr) uintptr { return proc(hwnd, m, w, l) })
	wc := wndClassEx{WndProc: cb, Instance: moduleHandle(), ClassName: u16(class)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return 0, err
	}
	h, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(class))), 0, 0, 0, 0, 0, 0, 0, moduleHandle(), 0)
	if h == 0 {
		return 0, err
	}
	return h, nil
}

func defProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(m), w, l)
	return r
}

func messageLoop() {
	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// MessageBox は簡単なメッセージを出す。
func MessageBox(title, text string) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), 0x40)
}

// TimeBeginPeriod はタイマーの精度を1msにする。
func TimeBeginPeriod() { pTimeBeginPeriod.Call(1) }

// Scale は画面の拡大率（100% = 1.0）。
func Scale() float64 {
	p := user32.NewProc("GetDpiForSystem")
	if p.Find() != nil {
		return 1
	}
	d, _, _ := p.Call()
	if d == 0 {
		return 1
	}
	return float64(d) / 96
}
