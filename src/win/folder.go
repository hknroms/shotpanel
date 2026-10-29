//go:build windows

package win

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                        = windows.NewLazySystemDLL("ole32.dll")
	pCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree               = ole32.NewProc("CoTaskMemFree")
	pCoInitializeEx              = ole32.NewProc("CoInitializeEx")
	pSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
)

var (
	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIShellItem       = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

// ErrCanceled は選ぶ画面を閉じたとき。
var ErrCanceled = errors.New("キャンセルしました")

func vcall(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(**[64]uintptr)(obj)
	r, _, _ := syscall.SyscallN(vtbl[idx], append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// PickFolder はWindowsのフォルダを選ぶ画面を出す（画面のスレッドから呼ぶ）。start は最初に開くフォルダ。
func PickFolder(owner uintptr, title, start string) (string, error) {
	pCoInitializeEx.Call(0, 2) // すでに初期化済みでも問題ない
	var dlg unsafe.Pointer
	if hr, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, 1|4,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg))); int32(hr) < 0 || dlg == nil {
		return "", errors.New("フォルダを選ぶ画面を開けませんでした")
	}
	defer vcall(dlg, 2) // Release
	var opts uint32
	vcall(dlg, 10, uintptr(unsafe.Pointer(&opts)))      // GetOptions
	vcall(dlg, 9, uintptr(opts|0x20|0x40|0x800))        // SetOptions: PICKFOLDERS | FORCEFILESYSTEM | PATHMUSTEXIST
	vcall(dlg, 17, uintptr(unsafe.Pointer(u16(title)))) // SetTitle
	if start != "" {
		var item unsafe.Pointer
		if hr, _, _ := pSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(u16(start))), 0, uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item))); int32(hr) >= 0 && item != nil {
			vcall(dlg, 12, uintptr(item)) // SetFolder
			vcall(item, 2)
		}
	}
	if hr := vcall(dlg, 3, owner); int32(hr) < 0 { // Show
		return "", ErrCanceled
	}
	var res unsafe.Pointer
	if hr := vcall(dlg, 20, uintptr(unsafe.Pointer(&res))); int32(hr) < 0 || res == nil { // GetResult
		return "", ErrCanceled
	}
	defer vcall(res, 2)
	var p *uint16
	if hr := vcall(res, 5, 0x80058000, uintptr(unsafe.Pointer(&p))); int32(hr) < 0 || p == nil { // GetDisplayName(SIGDN_FILESYSPATH)
		return "", errors.New("フォルダの場所を取得できませんでした")
	}
	defer pCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return windows.UTF16PtrToString(p), nil
}
