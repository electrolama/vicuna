package main

import (
	"sync"
	"time"
)

type event struct {
	Type      string        `json:"type"`
	Time      time.Time     `json:"time"`
	Direction string        `json:"direction,omitempty"`
	Data      []byte        `json:"data,omitempty"`
	Status    *serialStatus `json:"status,omitempty"`
	Signals   *modemSignals `json:"signals,omitempty"`
	Message   string        `json:"message,omitempty"`
}

type hub struct {
	mu          sync.RWMutex
	subscribers map[chan event]struct{}
}

func newHub() *hub {
	return &hub{subscribers: make(map[chan event]struct{})}
}

func (h *hub) subscribe() (<-chan event, func()) {
	ch := make(chan event, 128)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *hub) publish(value event) {
	if value.Time.IsZero() {
		value.Time = time.Now()
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- value:
		default:
			// A slow browser must not be allowed to stall the serial reader.
		}
	}
}
