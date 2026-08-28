package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

type serialConfig struct {
	Port     string `json:"port"`
	Baud     int    `json:"baud"`
	DataBits int    `json:"dataBits"`
	Parity   string `json:"parity"`
	StopBits string `json:"stopBits"`
	DTR      bool   `json:"dtr"`
	RTS      bool   `json:"rts"`
}

type serialStatus struct {
	Connected bool          `json:"connected"`
	Config    *serialConfig `json:"config,omitempty"`
	Error     string        `json:"error,omitempty"`
}

type modemSignals struct {
	Connected bool   `json:"connected"`
	Available bool   `json:"available"`
	DTR       bool   `json:"dtr"`
	RTS       bool   `json:"rts"`
	CTS       bool   `json:"cts"`
	DSR       bool   `json:"dsr"`
	RI        bool   `json:"ri"`
	DCD       bool   `json:"dcd"`
	Error     string `json:"error,omitempty"`
}

type portInfo struct {
	Name         string `json:"name"`
	USB          bool   `json:"usb"`
	VID          string `json:"vid,omitempty"`
	PID          string `json:"pid,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`
	Product      string `json:"product,omitempty"`
}

type detailedPortInfo struct {
	Name         string
	USB          bool
	VID          string
	PID          string
	SerialNumber string
	Product      string
}

type serialPort interface {
	io.ReadWriteCloser
	SetReadTimeout(time.Duration) error
	SetDTR(bool) error
	SetRTS(bool) error
	GetModemStatusBits() (*serial.ModemStatusBits, error)
	Break(time.Duration) error
	ResetInputBuffer() error
	ResetOutputBuffer() error
}

type serialOpener func(string, *serial.Mode) (serialPort, error)

type connection struct {
	port    serialPort
	config  serialConfig
	writeMu sync.Mutex
	once    sync.Once
}

func (c *connection) close() {
	c.once.Do(func() { _ = c.port.Close() })
}

type serialManager struct {
	mu      sync.RWMutex
	current *connection
	hub     *hub
	open    serialOpener
}

func newSerialManager(h *hub) *serialManager {
	return &serialManager{
		hub: h,
		open: func(name string, mode *serial.Mode) (serialPort, error) {
			return serial.Open(name, mode)
		},
	}
}

func (m *serialManager) Ports() ([]portInfo, error) {
	details, err := getDetailedPortsList()
	if err != nil {
		names, fallbackErr := serial.GetPortsList()
		if fallbackErr != nil {
			return nil, errors.Join(err, fallbackErr)
		}
		ports := make([]portInfo, 0, len(names))
		for _, name := range names {
			ports = append(ports, portInfo{Name: name})
		}
		sort.Slice(ports, func(i, j int) bool { return naturalPortLess(ports[i].Name, ports[j].Name) })
		return ports, nil
	}

	ports := make([]portInfo, 0, len(details))
	for _, detail := range details {
		ports = append(ports, portInfo{
			Name:         detail.Name,
			USB:          detail.USB,
			VID:          detail.VID,
			PID:          detail.PID,
			SerialNumber: detail.SerialNumber,
			Product:      detail.Product,
		})
	}
	sort.Slice(ports, func(i, j int) bool { return naturalPortLess(ports[i].Name, ports[j].Name) })
	return ports, nil
}

func (m *serialManager) Connect(config serialConfig) error {
	mode, err := modeFromConfig(&config)
	if err != nil {
		return err
	}
	port, err := m.open(config.Port, mode)
	if err != nil {
		return fmt.Errorf("open %s: %w", config.Port, err)
	}
	if err := port.SetReadTimeout(200 * time.Millisecond); err != nil {
		_ = port.Close()
		return fmt.Errorf("configure read timeout: %w", err)
	}
	if err := port.SetDTR(config.DTR); err != nil {
		_ = port.Close()
		return fmt.Errorf("set DTR: %w", err)
	}
	if err := port.SetRTS(config.RTS); err != nil {
		_ = port.Close()
		return fmt.Errorf("set RTS: %w", err)
	}

	next := &connection{port: port, config: config}
	m.mu.Lock()
	previous := m.current
	m.current = next
	m.mu.Unlock()
	if previous != nil {
		previous.close()
	}

	status := m.Status()
	m.hub.publish(event{Type: "status", Status: &status})
	go m.readLoop(next)
	go m.signalLoop(next)
	return nil
}

func (m *serialManager) Disconnect() {
	m.mu.Lock()
	previous := m.current
	m.current = nil
	m.mu.Unlock()
	if previous == nil {
		return
	}
	previous.close()
	status := m.Status()
	m.hub.publish(event{Type: "status", Status: &status})
	signals := m.Signals()
	m.hub.publish(event{Type: "signals", Signals: &signals})
}

func (m *serialManager) Status() serialStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return serialStatus{Connected: false}
	}
	config := m.current.config
	return serialStatus{Connected: true, Config: &config}
}

func (m *serialManager) Signals() modemSignals {
	m.mu.RLock()
	active := m.current
	m.mu.RUnlock()
	if active == nil {
		return modemSignals{}
	}
	signals, current := m.signalSnapshot(active)
	if !current {
		return modemSignals{}
	}
	return signals
}

func (m *serialManager) Write(data []byte) error {
	m.mu.RLock()
	active := m.current
	m.mu.RUnlock()
	if active == nil {
		return errors.New("serial port is not connected")
	}

	active.writeMu.Lock()
	defer active.writeMu.Unlock()
	written := 0
	for written < len(data) {
		n, err := active.port.Write(data[written:])
		if n > 0 {
			chunk := append([]byte(nil), data[written:written+n]...)
			m.hub.publish(event{Type: "data", Direction: "tx", Data: chunk})
			written += n
		}
		if err != nil {
			return fmt.Errorf("serial write: %w", err)
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (m *serialManager) SetSignals(dtr, rts *bool) error {
	m.mu.Lock()
	if m.current == nil {
		m.mu.Unlock()
		return errors.New("serial port is not connected")
	}
	active := m.current
	if dtr != nil {
		if err := active.port.SetDTR(*dtr); err != nil {
			m.mu.Unlock()
			return fmt.Errorf("set DTR: %w", err)
		}
		active.config.DTR = *dtr
	}
	if rts != nil {
		if err := active.port.SetRTS(*rts); err != nil {
			m.mu.Unlock()
			return fmt.Errorf("set RTS: %w", err)
		}
		active.config.RTS = *rts
	}
	status := serialStatus{Connected: true}
	config := active.config
	status.Config = &config
	m.mu.Unlock()
	m.hub.publish(event{Type: "status", Status: &status})
	if signals, current := m.signalSnapshot(active); current {
		m.hub.publish(event{Type: "signals", Signals: &signals})
	}
	return nil
}

func (m *serialManager) Break(duration time.Duration) error {
	m.mu.RLock()
	active := m.current
	m.mu.RUnlock()
	if active == nil {
		return errors.New("serial port is not connected")
	}
	if duration <= 0 || duration > 5*time.Second {
		return errors.New("break duration must be between 1ms and 5s")
	}
	return active.port.Break(duration)
}

func (m *serialManager) ResetBuffers() error {
	m.mu.RLock()
	active := m.current
	m.mu.RUnlock()
	if active == nil {
		return errors.New("serial port is not connected")
	}
	if err := active.port.ResetInputBuffer(); err != nil {
		return err
	}
	return active.port.ResetOutputBuffer()
}

func (m *serialManager) readLoop(active *connection) {
	buffer := make([]byte, 4096)
	for {
		n, err := active.port.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			m.hub.publish(event{Type: "data", Direction: "rx", Data: chunk})
		}
		if err != nil {
			m.mu.Lock()
			if m.current == active {
				m.current = nil
				m.mu.Unlock()
				active.close()
				status := serialStatus{Connected: false, Error: err.Error()}
				m.hub.publish(event{Type: "status", Status: &status, Message: err.Error()})
				signals := modemSignals{}
				m.hub.publish(event{Type: "signals", Signals: &signals})
			} else {
				m.mu.Unlock()
			}
			return
		}
	}
}

func (m *serialManager) signalLoop(active *connection) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var previous modemSignals
	first := true
	for {
		signals, current := m.signalSnapshot(active)
		if !current {
			return
		}
		if first || signals != previous {
			m.hub.publish(event{Type: "signals", Signals: &signals})
			previous = signals
			first = false
		}
		<-ticker.C
	}
}

func (m *serialManager) signalSnapshot(active *connection) (modemSignals, bool) {
	m.mu.RLock()
	if m.current != active {
		m.mu.RUnlock()
		return modemSignals{}, false
	}
	m.mu.RUnlock()

	bits, err := active.port.GetModemStatusBits()

	// The modem-status query may overlap a disconnect or a connection change.
	// Re-check ownership afterwards so a stale poll cannot make the browser look
	// connected again after the active port has gone away.
	m.mu.RLock()
	if m.current != active {
		m.mu.RUnlock()
		return modemSignals{}, false
	}
	config := active.config
	m.mu.RUnlock()

	signals := modemSignals{Connected: true, DTR: config.DTR, RTS: config.RTS}
	if err != nil {
		signals.Error = err.Error()
		return signals, true
	}
	if bits == nil {
		signals.Error = "serial driver returned no modem status"
		return signals, true
	}
	signals.Available = true
	signals.CTS = bits.CTS
	signals.DSR = bits.DSR
	signals.RI = bits.RI
	signals.DCD = bits.DCD
	return signals, true
}

func modeFromConfig(config *serialConfig) (*serial.Mode, error) {
	config.Port = strings.TrimSpace(config.Port)
	if config.Port == "" {
		return nil, errors.New("port is required")
	}
	if config.Baud < 50 || config.Baud > 12_000_000 {
		return nil, errors.New("baud rate must be between 50 and 12000000")
	}
	if config.DataBits < 5 || config.DataBits > 8 {
		return nil, errors.New("data bits must be between 5 and 8")
	}

	mode := &serial.Mode{
		BaudRate: config.Baud,
		DataBits: config.DataBits,
		InitialStatusBits: &serial.ModemOutputBits{
			DTR: config.DTR,
			RTS: config.RTS,
		},
	}
	switch strings.ToLower(config.Parity) {
	case "", "none":
		mode.Parity = serial.NoParity
		config.Parity = "none"
	case "odd":
		mode.Parity = serial.OddParity
	case "even":
		mode.Parity = serial.EvenParity
	case "mark":
		mode.Parity = serial.MarkParity
	case "space":
		mode.Parity = serial.SpaceParity
	default:
		return nil, errors.New("parity must be none, odd, even, mark, or space")
	}

	switch config.StopBits {
	case "", "1":
		mode.StopBits = serial.OneStopBit
		config.StopBits = "1"
	case "1.5":
		mode.StopBits = serial.OnePointFiveStopBits
	case "2":
		mode.StopBits = serial.TwoStopBits
	default:
		return nil, errors.New("stop bits must be 1, 1.5, or 2")
	}
	return mode, nil
}

func naturalPortLess(left, right string) bool {
	leftPrefix, leftNumber, leftNumeric := numericSuffix(strings.ToLower(left))
	rightPrefix, rightNumber, rightNumeric := numericSuffix(strings.ToLower(right))
	if leftNumeric && rightNumeric && leftPrefix == rightPrefix {
		return leftNumber < rightNumber
	}
	return strings.ToLower(left) < strings.ToLower(right)
}

func numericSuffix(value string) (string, int, bool) {
	index := len(value)
	for index > 0 && value[index-1] >= '0' && value[index-1] <= '9' {
		index--
	}
	if index == len(value) {
		return value, 0, false
	}
	number, err := strconv.Atoi(value[index:])
	return value[:index], number, err == nil
}
