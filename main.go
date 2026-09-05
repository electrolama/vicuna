package main

import (
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

var version = "dev"

//go:embed web/*
var webAssets embed.FS

func main() {
	os.Exit(mainExit(os.Args[1:]))
}

type launchOptions struct {
	listen     string
	configPath string
	console    bool
}

func mainExit(args []string) int {
	var options launchOptions
	var output bytes.Buffer
	flags := flag.NewFlagSet("vicuna", flag.ContinueOnError)
	flags.SetOutput(&output)
	flags.StringVar(&options.listen, "listen", "127.0.0.1:8080", "HTTP listen address")
	flags.StringVar(&options.configPath, "config", "", "JSON configuration path (default: discover vicuna.json)")
	showVersion := flags.Bool("version", false, "print version and exit")
	platformFlags(flags, &options)
	if err := flags.Parse(args); err != nil {
		writeCommandOutput(output.String(), !errors.Is(err, flag.ErrHelp))
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		writeCommandOutput("vicuna: unexpected argument: "+flags.Arg(0)+"\n", true)
		return 2
	}

	if *showVersion {
		writeCommandOutput(fmt.Sprintf("vicuna %s\n", version), false)
		return 0
	}
	if err := runPlatform(options); err != nil {
		reportPlatformError(err, options.console)
		return 1
	}
	return 0
}

// application owns the server and serial port independently of the platform UI.
type application struct {
	server   *http.Server
	manager  *serialManager
	url      string
	done     chan error
	stopOnce sync.Once
	mu       sync.Mutex
	stopping bool
	requests sync.WaitGroup
}

func startApplication(options launchOptions) (*application, error) {
	config, loadedConfigPath, err := loadDeploymentConfig(options.configPath)
	if err != nil {
		return nil, err
	}
	if loadedConfigPath != "" {
		log.Printf("loaded configuration from %s", loadedConfigPath)
	}

	static, err := fs.Sub(webAssets, "web")
	if err != nil {
		return nil, err
	}

	hub := newHub()
	manager := newSerialManager(hub)
	api := newAPIServer(manager, hub, http.FS(static), config)
	app := &application{manager: manager, done: make(chan error, 1)}
	app.server = &http.Server{
		Addr:              options.listen,
		Handler:           app.trackRequests(api.routes()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       75 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	listener, err := net.Listen("tcp", options.listen)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", options.listen, err)
	}
	// Use the bound port (including -listen :0), but preserve a supplied hostname.
	host, _, _ := net.SplitHostPort(options.listen)
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	app.url = (&url.URL{Scheme: "http", Host: displayAddress(net.JoinHostPort(host, port))}).String()
	log.Printf("vicuna %s listening at %s", version, app.url)
	go func() {
		err := app.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		app.done <- err
	}()
	return app, nil
}

func (a *application) trackRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		if a.stopping {
			a.mu.Unlock()
			http.Error(w, "Vicuna is stopping", http.StatusServiceUnavailable)
			return
		}
		a.requests.Add(1)
		a.mu.Unlock()
		defer a.requests.Done()
		next.ServeHTTP(w, r)
	})
}

func (a *application) Close() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		a.stopping = true
		a.mu.Unlock()
		// Close cancels long-lived event streams. Drain handlers before releasing
		// the serial port so an in-flight Connect cannot reopen it after Quit.
		_ = a.server.Close()
		a.requests.Wait()
		a.manager.Disconnect()
	})
}

func runConsole(options launchOptions) error {
	shutdown := make(chan os.Signal, 1)
	finished := make(chan struct{})
	stopSignals, err := notifyConsoleSignals(shutdown, finished)
	if err != nil {
		return err
	}
	defer stopSignals()
	defer close(finished)
	app, err := startApplication(options)
	if err != nil {
		return err
	}
	defer app.Close()
	select {
	case <-shutdown:
		app.Close()
		return <-app.done
	case err := <-app.done:
		return err
	}
}

func displayAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}
