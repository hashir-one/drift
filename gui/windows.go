//go:build windows

package main

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"sync"
	"syscall"
	"unsafe"
)

var user32dll = windows.NewLazySystemDLL("user32.dll")
var procEnumWindows = user32dll.NewProc("EnumWindows")
var procFindWindow = user32dll.NewProc("FindWindowW")
var procFindWindowEx = user32dll.NewProc("FindWindowExW")
var procSendMessage = user32dll.NewProc("SendMessageTimeoutW")
var procVisible = user32dll.NewProc("IsWindowVisible")
var procZoomed = user32dll.NewProc("IsZoomed")
var procStyle = user32dll.NewProc("GetWindowLongW")

// Enumeration is synchronous. Static callbacks and serialized state avoid both
// the finite callback table and passing Go pointers through LPARAM.
var desktopMu sync.Mutex
var desktopResult uintptr
var desktopCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	shell := windows.StringToUTF16Ptr("SHELLDLL_DefView")
	worker := windows.StringToUTF16Ptr("WorkerW")
	found, _, _ := procFindWindowEx.Call(hwnd, 0, uintptr(unsafe.Pointer(shell)), 0)
	if found != 0 {
		desktopResult, _, _ = procFindWindowEx.Call(0, hwnd, uintptr(unsafe.Pointer(worker)), 0)
	}
	return 1
})

func getDesktopWorkerW() (uintptr, error) {
	desktopMu.Lock()
	defer desktopMu.Unlock()
	name := windows.StringToUTF16Ptr("Progman")
	progman, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(name)), 0)
	if progman == 0 {
		return 0, errors.New("Windows desktop is not ready; try again in a moment")
	}
	var result uintptr
	ok, _, err := procSendMessage.Call(progman, 0x052c, 0, 0, 2, 1000, uintptr(unsafe.Pointer(&result)))
	if ok == 0 {
		return 0, fmt.Errorf("desktop did not respond: %v", err)
	}
	desktopResult = 0
	procEnumWindows.Call(desktopCallback, 0)
	if desktopResult == 0 {
		return 0, errors.New("Windows did not expose a wallpaper surface; refresh the desktop and retry")
	}
	return desktopResult, nil
}

var procForeground = user32dll.NewProc("GetForegroundWindow")
var procWindowPID = user32dll.NewProc("GetWindowThreadProcessId")
var procClassName = user32dll.NewProc("GetClassNameW")
var procCloaked = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")

type pauseWindow struct {
	visible, maximized, minimized, cloaked, own bool
	class                                       string
}

func shouldPauseWallpaper(w pauseWindow) bool {
	return w.visible && w.maximized && !w.minimized && !w.cloaked && !w.own && w.class != "Progman" && w.class != "WorkerW"
}

func foregroundAppMaximized() bool {
	hwnd, _, _ := procForeground.Call()
	if hwnd == 0 {
		return false
	}
	visible, _, _ := procVisible.Call(hwnd)
	zoomed, _, _ := procZoomed.Call(hwnd)
	if visible == 0 || zoomed == 0 {
		return false
	}
	var cloaked, pid uint32
	procCloaked.Call(hwnd, 14, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked))
	procWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	var class [256]uint16
	procClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	index := int32(-16)
	style, _, _ := procStyle.Call(hwnd, uintptr(index))
	return shouldPauseWallpaper(pauseWindow{visible: visible != 0, maximized: zoomed != 0, minimized: uint32(style)&0x20000000 != 0, cloaked: cloaked != 0, own: pid == windows.GetCurrentProcessId(), class: windows.UTF16ToString(class[:])})
}
func moveFile(src, dst string, replace bool) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(from, to, flags)
}
func isFileExists(err error) bool {
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS)
}
func singleInstance() (windows.Handle, error) {
	return windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(`Local\MLWStudio.Controller.v2`))
}
