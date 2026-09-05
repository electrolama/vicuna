package main

import (
	"fmt"
	"log"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The tray uses Win32 directly, keeping Windows builds CGO-free. All windows,
// menus and notification callbacks belong to the message-loop OS thread.
var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	shell32               = windows.NewLazySystemDLL("shell32.dll")
	registerClassEx       = user32.NewProc("RegisterClassExW")
	unregisterClass       = user32.NewProc("UnregisterClassW")
	createWindowEx        = user32.NewProc("CreateWindowExW")
	destroyWindow         = user32.NewProc("DestroyWindow")
	defWindowProc         = user32.NewProc("DefWindowProcW")
	getMessage            = user32.NewProc("GetMessageW")
	translateMessage      = user32.NewProc("TranslateMessage")
	dispatchMessage       = user32.NewProc("DispatchMessageW")
	postQuitMessage       = user32.NewProc("PostQuitMessage")
	postMessage           = user32.NewProc("PostMessageW")
	registerWindowMessage = user32.NewProc("RegisterWindowMessageW")
	setTimer              = user32.NewProc("SetTimer")
	createPopupMenu       = user32.NewProc("CreatePopupMenu")
	appendMenu            = user32.NewProc("AppendMenuW")
	destroyMenu           = user32.NewProc("DestroyMenu")
	setMenuDefaultItem    = user32.NewProc("SetMenuDefaultItem")
	trackPopupMenu        = user32.NewProc("TrackPopupMenu")
	setForegroundWindow   = user32.NewProc("SetForegroundWindow")
	getCursorPos          = user32.NewProc("GetCursorPos")
	messageBox            = user32.NewProc("MessageBoxW")
	createIcon            = user32.NewProc("CreateIcon")
	destroyIcon           = user32.NewProc("DestroyIcon")
	shellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
)

const (
	wmDestroy         = 0x0002
	wmClose           = 0x0010
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wmContextMenu     = 0x007b
	wmCommand         = 0x0111
	wmTimer           = 0x0113
	wmTray            = 0x8001
	ninSelect         = 0x0400
	ninKeySelect      = 0x0401
	menuOpen          = 1
	menuLogs          = 2
	menuQuit          = 3
)

type windowClass struct {
	size, style                   uint32
	procedure                     uintptr
	classExtra, windowExtra       int32
	instance, icon, cursor, brush windows.Handle
	menuName, className           *uint16
	smallIcon                     windows.Handle
}

type winPoint struct{ x, y int32 }

type windowMessage struct {
	window  windows.Handle
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	point   winPoint
	private uint32
}

type notificationIcon struct {
	size               uint32
	window             windows.Handle
	id, flags, message uint32
	icon               windows.Handle
	tip                [128]uint16
	state, stateMask   uint32
	info               [256]uint16
	version            uint32
	infoTitle          [64]uint16
	infoFlags          uint32
	guid               windows.GUID
	balloonIcon        windows.Handle
}

type windowsTray struct {
	app            *application
	instance       *desktopInstance
	logPath        string
	open           func(string) error
	window         windows.Handle
	icon           windows.Handle
	menu           windows.Handle
	taskbarCreated uint32
	serveDone      bool
	serveErr       error
	running        bool
}

func (t *windowsTray) run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := t.create(); err != nil {
		return err
	}
	defer t.dispose()
	if err := t.addIcon(); err != nil {
		return err
	}
	if timer, _, err := setTimer.Call(uintptr(t.window), 1, 250, 0); timer == 0 {
		return fmt.Errorf("create tray timer: %w", err)
	}
	t.running = true
	defer func() { t.running = false }()
	t.openPath(t.app.url)
	var message windowMessage
	for {
		result, _, err := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("read tray message: %w", err)
		}
		if result == 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&message)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
	t.app.Close()
	if !t.serveDone {
		t.serveErr = <-t.app.done
	}
	return t.serveErr
}

func (t *windowsTray) create() error {
	var module windows.Handle
	if err := windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, nil, &module); err != nil {
		return err
	}
	class := windowClass{
		instance:  module,
		className: windows.StringToUTF16Ptr("Vicuna.TrayWindow"),
		procedure: windows.NewCallback(t.windowProc),
	}
	class.size = uint32(unsafe.Sizeof(class))
	if atom, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("register tray window: %w", err)
	}
	// A hidden top-level window receives TaskbarCreated after Explorer restarts;
	// message-only windows do not receive that broadcast.
	window, _, err := createWindowEx.Call(0, uintptr(unsafe.Pointer(class.className)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Vicuña"))), 0,
		0, 0, 0, 0, 0, 0, uintptr(module), 0)
	if window == 0 {
		unregisterClass.Call(uintptr(unsafe.Pointer(class.className)), uintptr(module))
		return fmt.Errorf("create tray window: %w", err)
	}
	t.window = windows.Handle(window)
	message, _, err := registerWindowMessage.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
	if message == 0 {
		t.dispose()
		return fmt.Errorf("register taskbar message: %w", err)
	}
	t.taskbarCreated = uint32(message)
	t.icon, err = makeTrayIcon(module)
	if err != nil {
		t.dispose()
		return err
	}
	menu, _, err := createPopupMenu.Call()
	if menu == 0 {
		t.dispose()
		return fmt.Errorf("create tray menu: %w", err)
	}
	t.menu = windows.Handle(menu)
	for _, item := range []struct {
		id   uintptr
		text string
	}{{menuOpen, "&Open Vicuña"}, {menuLogs, "Open &logs"}, {0, ""}, {menuQuit, "&Quit"}} {
		flags := uintptr(0)
		if item.id == 0 {
			flags = 0x800 // MF_SEPARATOR
		}
		if ok, _, err := appendMenu.Call(menu, flags, item.id, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(item.text)))); ok == 0 {
			t.dispose()
			return fmt.Errorf("add tray menu item: %w", err)
		}
	}
	setMenuDefaultItem.Call(menu, menuOpen, 0)
	return nil
}

func (t *windowsTray) dispose() {
	if t.window != 0 {
		destroyWindow.Call(uintptr(t.window))
	}
	if t.menu != 0 {
		destroyMenu.Call(uintptr(t.menu))
		t.menu = 0
	}
	if t.icon != 0 {
		destroyIcon.Call(uintptr(t.icon))
		t.icon = 0
	}
	var module windows.Handle
	_ = windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, nil, &module)
	unregisterClass.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Vicuna.TrayWindow"))), uintptr(module))
}

func (t *windowsTray) iconData() notificationIcon {
	data := notificationIcon{window: t.window, id: 1, flags: 1 | 2 | 4 | 0x80, message: wmTray, icon: t.icon}
	data.size = uint32(unsafe.Sizeof(data))
	copy(data.tip[:127], windows.StringToUTF16("Vicuña "+version+" · "+t.app.url))
	return data
}

func (t *windowsTray) addIcon() error {
	data := t.iconData()
	if ok, _, _ := shellNotifyIcon.Call(0, uintptr(unsafe.Pointer(&data))); ok == 0 { // NIM_ADD
		return fmt.Errorf("could not add the Windows tray icon; use -console when no desktop is available")
	}
	// NOTIFYICON_VERSION_4 enables keyboard activation and context menus.
	data.version = 4
	if ok, _, _ := shellNotifyIcon.Call(4, uintptr(unsafe.Pointer(&data))); ok == 0 { // NIM_SETVERSION
		t.removeIcon()
		return fmt.Errorf("could not configure the Windows tray icon")
	}
	return nil
}

func (t *windowsTray) removeIcon() {
	data := t.iconData()
	shellNotifyIcon.Call(2, uintptr(unsafe.Pointer(&data))) // NIM_DELETE
}

func (t *windowsTray) windowProc(window, message, wparam, lparam uintptr) uintptr {
	switch uint32(message) {
	case wmTray:
		switch uint16(lparam) {
		case ninSelect, ninKeySelect:
			t.openPath(t.app.url)
		case wmContextMenu:
			t.showMenu()
		}
		return 0
	case wmCommand:
		t.command(uintptr(uint16(wparam)))
		return 0
	case wmTimer:
		if state, err := windows.WaitForSingleObject(t.instance.event, 0); err == nil && state == windows.WAIT_OBJECT_0 {
			t.openPath(t.app.url)
		}
		select {
		case t.serveErr = <-t.app.done:
			t.serveDone = true
			destroyWindow.Call(window)
		default:
		}
		return 0
	case wmQueryEndSession:
		return 1
	case wmEndSession:
		if wparam != 0 {
			// Windows may terminate us as soon as this callback returns.
			t.app.Close()
			destroyWindow.Call(window)
		}
		return 0
	case wmClose:
		destroyWindow.Call(window)
		return 0
	case wmDestroy:
		t.removeIcon()
		t.window = 0
		if t.running {
			postQuitMessage.Call(0)
		}
		return 0
	default:
		if t.taskbarCreated != 0 && uint32(message) == t.taskbarCreated {
			if err := t.addIcon(); err != nil {
				log.Print(err)
				showWindowsMessage(err.Error(), true)
				destroyWindow.Call(window)
			}
			return 0
		}
	}
	result, _, _ := defWindowProc.Call(window, message, wparam, lparam)
	return result
}

func (t *windowsTray) showMenu() {
	var point winPoint
	if ok, _, _ := getCursorPos.Call(uintptr(unsafe.Pointer(&point))); ok == 0 {
		return
	}
	setForegroundWindow.Call(uintptr(t.window))
	command, _, _ := trackPopupMenu.Call(uintptr(t.menu), 0x100|0x2, // TPM_RETURNCMD | TPM_RIGHTBUTTON
		uintptr(point.x), uintptr(point.y), 0, uintptr(t.window), 0)
	postMessage.Call(uintptr(t.window), 0, 0, 0) // WM_NULL: dismiss reliably on the next click
	t.command(command)
}

func (t *windowsTray) command(command uintptr) {
	switch command {
	case menuOpen:
		t.openPath(t.app.url)
	case menuLogs:
		t.openPath(t.logPath)
	case menuQuit:
		destroyWindow.Call(uintptr(t.window))
	}
}

func (t *windowsTray) openPath(path string) {
	if err := t.open(path); err != nil {
		log.Printf("open %s: %v", path, err)
		showWindowsMessage(fmt.Sprintf("Could not open %s\n\n%v", path, err), true)
	}
}

// Draw the existing blue $_ terminal brand as a small pixel icon. Keeping the
// artwork in Go avoids a runtime asset file or a platform resource compiler.
func makeTrayIcon(module windows.Handle) (windows.Handle, error) {
	const size = 32
	mask := make([]byte, size*size/8)
	pixels := make([]byte, size*size*4)
	mark := [...]string{
		"  #         ",
		" ####       ",
		"# #         ",
		" ###        ",
		"  # #       ",
		"####        ",
		"  #   ##### ",
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			offset := (y*size + x) * 4
			color := [4]byte{0x26, 0x25, 0x25, 0xff} // BGRA, matching the web brand
			if x < 2 || x >= size-2 || y < 2 || y >= size-2 {
				color = [4]byte{0x5e, 0x3c, 0x17, 0xff}
			}
			mx, my := (x-4)/2, (y-8)/2
			if x >= 4 && y >= 8 && my < len(mark) && mx < len(mark[my]) && mark[my][mx] == '#' {
				color = [4]byte{0xfc, 0xaa, 0x4d, 0xff}
			}
			copy(pixels[offset:offset+4], color[:])
		}
	}
	icon, _, err := createIcon.Call(uintptr(module), size, size, 1, 32,
		uintptr(unsafe.Pointer(&mask[0])), uintptr(unsafe.Pointer(&pixels[0])))
	if icon == 0 {
		return 0, fmt.Errorf("create tray icon: %w", err)
	}
	return windows.Handle(icon), nil
}
