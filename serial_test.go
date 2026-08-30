package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"go.bug.st/serial"
)

type statusTestPort struct {
	started chan struct{}
	release chan struct{}
	closed  bool
}

func (p *statusTestPort) Read([]byte) (int, error)           { return 0, io.EOF }
func (p *statusTestPort) Write(value []byte) (int, error)    { return len(value), nil }
func (p *statusTestPort) Close() error                       { p.closed = true; return nil }
func (p *statusTestPort) SetReadTimeout(time.Duration) error { return nil }
func (p *statusTestPort) SetDTR(bool) error                  { return nil }
func (p *statusTestPort) SetRTS(bool) error                  { return nil }
func (p *statusTestPort) Break(time.Duration) error          { return nil }
func (p *statusTestPort) ResetInputBuffer() error            { return nil }
func (p *statusTestPort) ResetOutputBuffer() error           { return nil }
func (p *statusTestPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	close(p.started)
	<-p.release
	return &serial.ModemStatusBits{CTS: true}, nil
}

func TestModeFromConfig(t *testing.T) {
	config := serialConfig{Port: " COM4 ", Baud: 115200, DataBits: 8, Parity: "even", StopBits: "2", DTR: true, RTS: false}
	mode, err := modeFromConfig(&config)
	if err != nil {
		t.Fatal(err)
	}
	if config.Port != "COM4" {
		t.Fatalf("port was not trimmed: %q", config.Port)
	}
	if mode.BaudRate != 115200 || mode.DataBits != 8 || mode.Parity != serial.EvenParity || mode.StopBits != serial.TwoStopBits {
		t.Fatalf("unexpected mode: %+v", mode)
	}
	if mode.InitialStatusBits == nil || !mode.InitialStatusBits.DTR || mode.InitialStatusBits.RTS {
		t.Fatalf("unexpected initial modem signals: %+v", mode.InitialStatusBits)
	}
}

func TestModeFromConfigRejectsInvalidValues(t *testing.T) {
	tests := []serialConfig{
		{Port: "", Baud: 115200, DataBits: 8, Parity: "none", StopBits: "1"},
		{Port: "COM1", Baud: 0, DataBits: 8, Parity: "none", StopBits: "1"},
		{Port: "COM1", Baud: 115200, DataBits: 9, Parity: "none", StopBits: "1"},
		{Port: "COM1", Baud: 115200, DataBits: 8, Parity: "banana", StopBits: "1"},
		{Port: "COM1", Baud: 115200, DataBits: 8, Parity: "none", StopBits: "3"},
	}
	for _, config := range tests {
		if _, err := modeFromConfig(&config); err == nil {
			t.Fatalf("expected config to fail: %+v", config)
		}
	}
}

func TestNaturalPortOrder(t *testing.T) {
	if !naturalPortLess("COM2", "COM10") {
		t.Fatal("COM2 should sort before COM10")
	}
	if naturalPortLess("COM10", "COM2") {
		t.Fatal("COM10 should not sort before COM2")
	}
}

func TestSignalSnapshotDiscardsPollFromDisconnectedPort(t *testing.T) {
	port := &statusTestPort{started: make(chan struct{}), release: make(chan struct{})}
	active := &connection{port: port, config: serialConfig{DTR: true}}
	manager := &serialManager{current: active, hub: newHub()}

	result := make(chan bool, 1)
	go func() {
		_, current := manager.signalSnapshot(active)
		result <- current
	}()
	<-port.started
	manager.mu.Lock()
	manager.current = nil
	manager.mu.Unlock()
	close(port.release)

	select {
	case current := <-result:
		if current {
			t.Fatal("stale modem poll was reported as current after disconnect")
		}
	case <-time.After(time.Second):
		t.Fatal("signal snapshot did not complete")
	}
}

func TestMissingPortDisconnectsActiveConnection(t *testing.T) {
	port := &statusTestPort{}
	active := &connection{port: port, config: serialConfig{Port: "COM7"}}
	events := newHub()
	updates, unsubscribe := events.subscribe()
	defer unsubscribe()
	manager := &serialManager{
		current: active,
		hub:     events,
		ports:   func() ([]portInfo, error) { return []portInfo{{Name: "COM8"}}, nil },
	}

	if manager.checkPortPresence(active) {
		t.Fatal("presence check should stop after the active port disappears")
	}
	if manager.Status().Connected {
		t.Fatal("manager still reports a connection to a missing port")
	}
	if !port.closed {
		t.Fatal("missing port handle was not closed")
	}

	statusEvent := <-updates
	if statusEvent.Status == nil || statusEvent.Status.Connected || !strings.Contains(statusEvent.Status.Error, "COM7") {
		t.Fatalf("unexpected disconnect event: %+v", statusEvent)
	}
	signalsEvent := <-updates
	if signalsEvent.Signals == nil || signalsEvent.Signals.Connected {
		t.Fatalf("unexpected signals event: %+v", signalsEvent)
	}
}

func TestPortPresenceIgnoresTransientEnumerationFailure(t *testing.T) {
	port := &statusTestPort{}
	active := &connection{port: port, config: serialConfig{Port: "COM7"}}
	manager := &serialManager{
		current: active,
		hub:     newHub(),
		ports:   func() ([]portInfo, error) { return nil, errors.New("registry busy") },
	}

	if !manager.checkPortPresence(active) {
		t.Fatal("transient enumeration failure stopped presence checks")
	}
	if !manager.Status().Connected || port.closed {
		t.Fatal("transient enumeration failure disconnected the active port")
	}
}
