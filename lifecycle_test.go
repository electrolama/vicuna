package main

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfigPath(t *testing.T) string {
	t.Helper()
	config := filepath.Join(t.TempDir(), "vicuna.json")
	if err := os.WriteFile(config, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	return config
}

func testApplication(t *testing.T) *application {
	t.Helper()
	app, err := startApplication(launchOptions{listen: "127.0.0.1:0", configPath: testConfigPath(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return app
}

func TestApplicationReadyAndOccupiedPort(t *testing.T) {
	app := testApplication(t)
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(app.url + "/api/about")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || strings.HasSuffix(app.url, ":0") {
		t.Fatalf("listener not ready: %s, status %d", app.url, response.StatusCode)
	}
	other, err := startApplication(launchOptions{
		listen:     strings.TrimPrefix(app.url, "http://"),
		configPath: testConfigPath(t),
	})
	if err == nil {
		other.Close()
		t.Fatal("occupied port was not reported during startup")
	}
	if !strings.Contains(err.Error(), "listen on") {
		t.Fatalf("unexpected startup error: %v", err)
	}
}

func TestApplicationClosesEventStreamAndSerialPort(t *testing.T) {
	app := testApplication(t)
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(app.url + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	port := &statusTestPort{}
	app.manager.mu.Lock()
	app.manager.current = &connection{port: port}
	app.manager.mu.Unlock()
	closed := make(chan struct{})
	go func() { app.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel the event stream")
	}
	if !port.closed || app.manager.Status().Connected {
		t.Fatal("shutdown left the serial port connected")
	}
	if err := <-app.done; err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, reader)
	app.Close() // Repeated quit/cleanup must be safe.
}

func TestShutdownDrainsConnectBeforeDisconnect(t *testing.T) {
	app := &application{manager: newSerialManager(newHub())}
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	port := &statusTestPort{}
	handler := app.trackRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		<-release // Simulate a device opening after the connection was cancelled.
		app.manager.mu.Lock()
		app.manager.current = &connection{port: port}
		app.manager.mu.Unlock()
	}))
	server := httptest.NewServer(handler)
	defer func() {
		releaseHandler()
		server.Close()
	}()
	app.server = server.Config
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := server.Client().Get(server.URL)
		if err == nil {
			response.Body.Close()
		}
	}()
	<-started
	closed := make(chan struct{})
	go func() { app.Close(); close(closed) }()
	<-cancelled
	select {
	case <-closed:
		t.Fatal("shutdown returned while a device was still opening")
	default:
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("new request during shutdown returned %d", response.Code)
	}
	releaseHandler()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish after the request drained")
	}
	<-requestDone
	if !port.closed || app.manager.Status().Connected {
		t.Fatal("a late connection escaped shutdown")
	}
}
