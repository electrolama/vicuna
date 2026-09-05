package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	attachConsole         = kernel32.NewProc("AttachConsole")
	allocConsole          = kernel32.NewProc("AllocConsole")
	freeConsole           = kernel32.NewProc("FreeConsole")
	setConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
)

func platformFlags(flags *flag.FlagSet, options *launchOptions) {
	flags.BoolVar(&options.console, "console", false, "run in a console without the Windows tray or automatic browser launch")
}

func runPlatform(options launchOptions) error {
	if options.console {
		if err := connectConsole(true); err != nil {
			return err
		}
		return runConsole(options)
	}
	// Also detach a console-subsystem development build. Never hide a shared
	// console window: it may belong to the user's terminal.
	freeConsole.Call()
	instance, existing, err := acquireInstance(options.listen)
	if err != nil {
		return err
	}
	if existing {
		return nil
	}
	defer instance.Close()
	logPath, err := windowsLogPath(options.listen)
	if err != nil {
		return err
	}
	file, err := openWindowsLog(logPath)
	if err != nil {
		return err
	}
	previousOutput := log.Writer()
	log.SetOutput(file)
	defer func() {
		log.SetOutput(previousOutput)
		_ = file.Close()
	}()
	app, err := startApplication(options)
	if err == nil {
		defer app.Close()
		tray := &windowsTray{app: app, instance: instance, logPath: logPath, open: shellOpen}
		err = tray.run()
	}
	if err != nil {
		log.Print(err)
		return fmt.Errorf("%w\n\nLog: %s", err, logPath)
	}
	log.Print("vicuna stopped")
	return nil
}

// Attach GUI releases to the invoking terminal for -console, -help and
// -version. Preserve inherited redirected handles (including PowerShell pipes).
func connectConsole(create bool) error {
	stdout, stderr := os.Stdout, os.Stderr
	_, stdoutErr := stdout.Stat()
	_, stderrErr := stderr.Stat()
	ok, _, callErr := attachConsole.Call(uintptr(^uint32(0))) // ATTACH_PARENT_PROCESS
	if ok == 0 && !errors.Is(callErr, windows.ERROR_ACCESS_DENIED) && create {
		if ok, _, err := allocConsole.Call(); ok == 0 {
			return fmt.Errorf("create console: %w", err)
		}
	}
	if stdoutErr != nil {
		if handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && handle != 0 {
			os.Stdout = os.NewFile(uintptr(handle), "stdout")
		}
	}
	if stderrErr != nil {
		if handle, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE); err == nil && handle != 0 {
			os.Stderr = os.NewFile(uintptr(handle), "stderr")
		}
	}
	log.SetOutput(os.Stderr)
	return nil
}

func writeCommandOutput(text string, isError bool) {
	_ = connectConsole(false)
	out := os.Stdout
	if isError {
		out = os.Stderr
	}
	if _, err := fmt.Fprint(out, text); err != nil {
		showWindowsMessage(text, isError)
	}
}

// AttachConsole/AllocConsole reset the handler table installed by the Go
// runtime. Install our own handler afterwards so GUI releases can stop cleanly
// in -console mode too. Windows terminates the process when a close handler
// returns, so keep that callback alive until cleanup has completed.
func notifyConsoleSignals(shutdown chan os.Signal, finished <-chan struct{}) (func(), error) {
	handler := windows.NewCallback(func(event uintptr) uintptr {
		switch event {
		case windows.CTRL_C_EVENT, windows.CTRL_BREAK_EVENT, windows.CTRL_CLOSE_EVENT,
			windows.CTRL_LOGOFF_EVENT, windows.CTRL_SHUTDOWN_EVENT:
			select {
			case shutdown <- os.Interrupt:
			default:
			}
			if event != windows.CTRL_C_EVENT && event != windows.CTRL_BREAK_EVENT {
				<-finished
			}
			return 1
		default:
			return 0
		}
	})
	if ok, _, err := setConsoleCtrlHandler.Call(handler, 1); ok == 0 {
		return nil, fmt.Errorf("install console shutdown handler: %w", err)
	}
	return func() { setConsoleCtrlHandler.Call(handler, 0) }, nil
}

func reportPlatformError(err error, console bool) {
	if console {
		log.Print(err)
		return
	}
	showWindowsMessage(err.Error(), true)
}

func showWindowsMessage(text string, isError bool) {
	style := uintptr(0x40) // MB_ICONINFORMATION
	if isError {
		style = 0x10 // MB_ICONERROR
	}
	messageBox.Call(0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Vicuña"))), style)
}

func shellOpen(path string) error {
	value, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), value, nil, nil, windows.SW_SHOWNORMAL)
}

type desktopInstance struct {
	mutex windows.Handle
	event windows.Handle
}

// Scope instances to the current user/session and the requested listen address.
// An auto-reset event retains a second launch even while the first is starting;
// it does not need an unauthenticated HTTP endpoint or a PID/URL file.
func acquireInstance(listen string) (*desktopInstance, bool, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, false, fmt.Errorf("identify Windows user: %w", err)
	}
	key := fmt.Sprintf("Local\\Vicuna-%x", sha256.Sum256([]byte(user.User.Sid.String()+"\x00"+listen)))
	return acquireNamedInstance(key)
}

func acquireNamedInstance(key string) (*desktopInstance, bool, error) {
	event, err := windows.CreateEvent(nil, 0, 0, windows.StringToUTF16Ptr(key+"-open"))
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, false, fmt.Errorf("create instance event: %w", err)
	}
	instance := &desktopInstance{event: event}
	mutex, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(key))
	instance.mutex = mutex
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		defer instance.Close()
		if err := windows.SetEvent(event); err != nil {
			return nil, false, fmt.Errorf("activate existing Vicuna: %w", err)
		}
		return nil, true, nil
	}
	if err != nil {
		instance.Close()
		return nil, false, fmt.Errorf("create instance mutex: %w", err)
	}
	return instance, false, nil
}

func (i *desktopInstance) Close() {
	if i.mutex != 0 {
		_ = windows.CloseHandle(i.mutex)
		i.mutex = 0
	}
	if i.event != 0 {
		_ = windows.CloseHandle(i.event)
		i.event = 0
	}
}

func windowsLogPath(listen string) (string, error) {
	directory, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", fmt.Errorf("locate local application data: %w", err)
	}
	name := "vicuna.log"
	if listen != "127.0.0.1:8080" {
		digest := sha256.Sum256([]byte(listen))
		name = fmt.Sprintf("vicuna-%x.log", digest[:6])
	}
	return filepath.Join(directory, "Vicuna", "logs", name), nil
}

// Retain one previous log once a log reaches 5 MiB. Rotation happens at startup;
// these logs contain lifecycle/launcher diagnostics, not the serial byte stream.
func openWindowsLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect log: %w", err)
	}
	if err == nil && info.Size() >= 5<<20 {
		if err := os.Remove(path + ".1"); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove previous log: %w", err)
		}
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, fmt.Errorf("rotate log: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	return file, nil
}
