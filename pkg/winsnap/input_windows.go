//go:build windows

package winsnap

import (
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procOpenClipboard    = modUser32.NewProc("OpenClipboard")
	procCloseClipboard   = modUser32.NewProc("CloseClipboard")
	procEmptyClipboard   = modUser32.NewProc("EmptyClipboard")
	procSetClipboardData = modUser32.NewProc("SetClipboardData")
	procGlobalAlloc      = modKernel32.NewProc("GlobalAlloc")
	procGlobalLock       = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock     = modKernel32.NewProc("GlobalUnlock")
	procSendInput        = modUser32.NewProc("SendInput")
	procSetWindowPosIn   = modUser32.NewProc("SetWindowPos")
)

const (
	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002

	// SendInput constants
	INPUT_KEYBOARD   = 1
	KEYEVENTF_KEYUP  = 0x0002
	VK_CONTROL       = 0x11
	VK_RETURN        = 0x0D
	VK_V             = 0x56

	// SetWindowPos constants
	HWND_TOP_INPUT    = 0
	SWP_NOMOVE_INPUT  = 0x0002
	SWP_NOSIZE_INPUT  = 0x0001
	SWP_SHOWWINDOW_IN = 0x0040
)

// KEYBDINPUT structure for SendInput
type keyboardInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

// INPUT structure for SendInput
type inputUnion struct {
	inputType uint32
	ki        keyboardInput
	padding   [8]byte // Padding to match the C union size
}

// SendTextToTarget sends text to the target application by:
// 1. Copying text to clipboard
// 2. Bringing target window to front (without stealing focus from Wails)
// 3. Simulating Ctrl+V to paste using SendInput
// 4. Optionally simulating Enter or Ctrl+Enter to send
func SendTextToTarget(targetProcess string, text string, triggerSend bool, sendKeyStrategy string) error {
	if targetProcess == "" {
		return errors.New("winsnap: target process is empty")
	}
	if text == "" {
		return errors.New("winsnap: text is empty")
	}

	// Find target window
	targetNames := expandWindowsTargetNames(targetProcess)
	if len(targetNames) == 0 {
		return errors.New("winsnap: invalid target process name")
	}

	var targetHWND windows.HWND
	for _, name := range targetNames {
		h, err := findMainWindowByProcessName(name)
		if err == nil && h != 0 {
			targetHWND = h
			break
		}
	}
	if targetHWND == 0 {
		return ErrTargetWindowNotFound
	}

	// Copy text to clipboard first
	if err := setClipboardText(text); err != nil {
		return err
	}

	// Use the proven wake method to activate target - same as WakeAttachedWindow
	// This method is already tested and works with the snap functionality
	activateHwndInput(targetHWND)
	time.Sleep(200 * time.Millisecond)

	// Simulate Ctrl+V to paste using SendInput
	sendCtrlV()
	time.Sleep(100 * time.Millisecond)

	// Optionally trigger send
	if triggerSend {
		time.Sleep(150 * time.Millisecond)
		if sendKeyStrategy == "ctrl_enter" {
			sendCtrlEnter()
		} else {
			sendEnter()
		}
	}

	return nil
}

// PasteTextToTarget sends text to the target application's edit box without triggering send.
func PasteTextToTarget(targetProcess string, text string) error {
	return SendTextToTarget(targetProcess, text, false, "")
}

// activateHwndInput activates the target window using the same proven method from wake_windows.go
// This is a copy of activateHwnd to avoid import cycles
func activateHwndInput(hwnd windows.HWND) {
	if hwnd == 0 {
		return
	}

	foregroundHwnd, _, _ := procGetForegroundWindowWake.Call()
	var attached bool
	var foregroundTid, currentTid uintptr

	if foregroundHwnd != 0 {
		var foregroundPid uint32
		foregroundTid, _, _ = procGetWindowThreadProcIdWake.Call(
			foregroundHwnd,
			uintptr(unsafe.Pointer(&foregroundPid)),
		)
		currentTid, _, _ = procGetCurrentThreadIdWake.Call()
		if foregroundTid != currentTid {
			ret, _, _ := procAttachThreadInputWake.Call(currentTid, foregroundTid, 1)
			attached = ret != 0
		}
	}

	procShowWindowWake.Call(uintptr(hwnd), swRestoreWake)
	procSetForegroundWindowWake.Call(uintptr(hwnd))
	procBringWindowToTopWake.Call(uintptr(hwnd))

	if attached {
		procAttachThreadInputWake.Call(currentTid, foregroundTid, 0)
	}
}

func setClipboardText(text string) error {
	// Convert to UTF-16
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}

	// Open clipboard with retry
	var ret uintptr
	for i := 0; i < 10; i++ {
		ret, _, _ = procOpenClipboard.Call(0)
		if ret != 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if ret == 0 {
		return errors.New("winsnap: failed to open clipboard")
	}
	defer procCloseClipboard.Call()

	// Empty clipboard
	procEmptyClipboard.Call()

	// Allocate global memory
	size := len(utf16) * 2
	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	if hMem == 0 {
		return errors.New("winsnap: failed to allocate memory")
	}

	// Lock and copy data
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return errors.New("winsnap: failed to lock memory")
	}

	// Copy UTF-16 data
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(utf16))
	copy(dst, utf16)

	procGlobalUnlock.Call(hMem)

	// Set clipboard data
	ret, _, _ = procSetClipboardData.Call(CF_UNICODETEXT, hMem)
	if ret == 0 {
		return errors.New("winsnap: failed to set clipboard data")
	}

	return nil
}

// sendInput sends keyboard input using SendInput API
func sendKeyboardInput(inputs []inputUnion) {
	if len(inputs) == 0 {
		return
	}
	procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
}

// makeKeyDown creates a key down input
func makeKeyDown(vk uint16) inputUnion {
	return inputUnion{
		inputType: INPUT_KEYBOARD,
		ki: keyboardInput{
			wVk:     vk,
			dwFlags: 0,
		},
	}
}

// makeKeyUp creates a key up input
func makeKeyUp(vk uint16) inputUnion {
	return inputUnion{
		inputType: INPUT_KEYBOARD,
		ki: keyboardInput{
			wVk:     vk,
			dwFlags: KEYEVENTF_KEYUP,
		},
	}
}

func sendCtrlV() {
	// Send all key events: Ctrl down, V down, V up, Ctrl up
	inputs := []inputUnion{
		makeKeyDown(VK_CONTROL),
		makeKeyDown(VK_V),
		makeKeyUp(VK_V),
		makeKeyUp(VK_CONTROL),
	}
	sendKeyboardInput(inputs)
}

func sendEnter() {
	inputs := []inputUnion{
		makeKeyDown(VK_RETURN),
		makeKeyUp(VK_RETURN),
	}
	sendKeyboardInput(inputs)
}

func sendCtrlEnter() {
	inputs := []inputUnion{
		makeKeyDown(VK_CONTROL),
		makeKeyDown(VK_RETURN),
		makeKeyUp(VK_RETURN),
		makeKeyUp(VK_CONTROL),
	}
	sendKeyboardInput(inputs)
}
