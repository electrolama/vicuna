package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsInstanceHelper(t *testing.T) {
	key := os.Getenv("VICUNA_TEST_INSTANCE")
	if key == "" {
		return
	}
	instance, existing, err := acquireNamedInstance(key)
	if instance != nil {
		instance.Close()
	}
	if err != nil || !existing {
		t.Fatalf("expected existing instance: %t, %v", existing, err)
	}
}

func TestWindowsConsoleHelper(t *testing.T) {
	if os.Getenv("VICUNA_TEST_CONSOLE") == "" {
		return
	}
	// Allocate a private hidden console directly, so this test never attaches
	// to the parent's terminal. Allocation reproduces the GUI release's reset
	// of Go's original control handler.
	freeConsole.Call()
	if ok, _, err := allocConsole.Call(); ok == 0 {
		t.Fatal(err)
	}
	shutdown := make(chan os.Signal, 1)
	finished := make(chan struct{})
	stop, err := notifyConsoleSignals(shutdown, finished)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	defer close(finished)
	if ok, _, err := kernel32.NewProc("GenerateConsoleCtrlEvent").Call(windows.CTRL_BREAK_EVENT, 0); ok == 0 {
		t.Fatal(err)
	}
	select {
	case <-shutdown:
	case <-time.After(3 * time.Second):
		t.Fatal("console control event did not reach shutdown handler")
	}
}

func TestWindowsConsoleAfterAllocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsConsoleHelper$")
	command.Env = append(os.Environ(), "VICUNA_TEST_CONSOLE=1")
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("console shutdown helper: %v\n%s", err, output)
	}
}

func TestWindowsInstanceActivationAndRelease(t *testing.T) {
	key := fmt.Sprintf("Local\\Vicuna-Test-%d-%d", os.Getpid(), time.Now().UnixNano())
	instance, existing, err := acquireNamedInstance(key)
	if err != nil || existing {
		t.Fatalf("first instance: existing=%t, err=%v", existing, err)
	}
	defer instance.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsInstanceHelper$")
	command.Env = append(os.Environ(), "VICUNA_TEST_INSTANCE="+key)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("second launch: %v\n%s", err, output)
	}
	if state, err := windows.WaitForSingleObject(instance.event, 0); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("second launch was not retained before the tray was ready: state=%d, err=%v", state, err)
	}
	if state, _ := windows.WaitForSingleObject(instance.event, 0); state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("activation was not consumed: %d", state)
	}
	instance.Close()
	next, existing, err := acquireNamedInstance(key)
	if err != nil || existing {
		t.Fatalf("restart after quit: existing=%t, err=%v", existing, err)
	}
	next.Close()
}

func TestWindowsLogRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "vicuna.log")
	file, err := openWindowsLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(make([]byte, 5<<20)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	if err := os.WriteFile(path+".1", []byte("older log"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err = openWindowsLog(path)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	info, err := os.Stat(path + ".1")
	if err != nil || info.Size() != 5<<20 {
		t.Fatalf("rotated log: %v, %v", info, err)
	}
	info, err = os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatalf("fresh log: %v, %v", info, err)
	}
}

func TestWindowsNativeTrayCommands(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var opened []string
	tray := &windowsTray{
		app: &application{url: "http://127.0.0.1:9123"}, logPath: `C:\Vicuna\vicuna.log`,
		open: func(path string) error { opened = append(opened, path); return nil },
	}
	if err := tray.create(); err != nil {
		t.Fatal(err)
	}
	defer tray.dispose()
	if visible, _, _ := user32.NewProc("IsWindowVisible").Call(uintptr(tray.window)); visible != 0 {
		t.Fatal("tray host window must remain hidden")
	}
	if count, _, _ := user32.NewProc("GetMenuItemCount").Call(uintptr(tray.menu)); count != 4 {
		t.Fatalf("unexpected native menu item count: %d", count)
	}
	send := user32.NewProc("SendMessageW")
	send.Call(uintptr(tray.window), wmCommand, menuOpen, 0)
	send.Call(uintptr(tray.window), wmCommand, menuLogs, 0)
	send.Call(uintptr(tray.window), wmTray, 0, ninKeySelect)
	if len(opened) != 3 || opened[0] != tray.app.url || opened[1] != tray.logPath || opened[2] != tray.app.url {
		t.Fatalf("unexpected tray actions: %v", opened)
	}
	send.Call(uintptr(tray.window), wmCommand, menuQuit, 0)
	if tray.window != 0 {
		t.Fatal("Quit did not destroy the tray window")
	}
}

func TestWindowsTrayLifecycle(t *testing.T) {
	// Hosted CI may have no Explorer desktop. Native window/command and
	// cross-process activation tests above still run in that environment.
	findWindow := user32.NewProc("FindWindowW")
	if shell, _, _ := findWindow.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Shell_TrayWnd"))), 0); shell == 0 {
		t.Skip("Explorer desktop is unavailable")
	}
	app := testApplication(t)
	key := fmt.Sprintf("Local\\Vicuna-TrayTest-%d-%d", os.Getpid(), time.Now().UnixNano())
	instance, _, err := acquireNamedInstance(key)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	tray := &windowsTray{app: app, instance: instance}
	opened := 0
	tray.open = func(path string) error {
		opened++
		if path != app.url {
			t.Errorf("opened %q instead of %q", path, app.url)
		}
		if opened == 1 {
			// Exercise icon recovery without restarting the user's Explorer.
			tray.removeIcon()
			postMessage.Call(uintptr(tray.window), uintptr(tray.taskbarCreated), 0, 0)
			if err := windows.SetEvent(instance.event); err != nil {
				t.Error(err)
			}
		} else {
			postMessage.Call(uintptr(tray.window), wmCommand, menuQuit, 0)
		}
		return nil
	}
	// A failed timer/activation must not leave the test's server running forever.
	watchdog := time.AfterFunc(5*time.Second, app.Close)
	defer watchdog.Stop()
	if err := tray.run(); err != nil {
		t.Fatal(err)
	}
	if opened != 2 || tray.window != 0 {
		t.Fatalf("tray lifecycle: opened=%d, window=%d", opened, tray.window)
	}
	app.mu.Lock()
	stopped := app.stopping
	app.mu.Unlock()
	if !stopped {
		t.Fatal("tray Quit did not stop the application")
	}
}
