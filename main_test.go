package main

import (
	"strings"
	"testing"
)

func TestDisplayAddress(t *testing.T) {
	tests := map[string]string{
		":8080":          "localhost:8080",
		"0.0.0.0:9090":   "localhost:9090",
		"[::]:7000":      "localhost:7000",
		"127.0.0.1:8080": "127.0.0.1:8080",
		"[::1]:8080":     "[::1]:8080",
		"not-an-address": "not-an-address",
	}
	for input, expected := range tests {
		if got := displayAddress(input); got != expected {
			t.Errorf("displayAddress(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestEmbeddedTerminalAssets(t *testing.T) {
	for _, path := range []string{"web/xterm.js", "web/xterm-addon-fit.js", "web/xterm.css"} {
		contents, err := webAssets.ReadFile(path)
		if err != nil {
			t.Fatalf("read embedded %s: %v", path, err)
		}
		if len(contents) == 0 {
			t.Fatalf("embedded %s is empty", path)
		}
	}

	index, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(index)
	positions := []int{
		strings.Index(page, `src="xterm.js"`),
		strings.Index(page, `src="xterm-addon-fit.js"`),
		strings.Index(page, `src="app.js"`),
	}
	if positions[0] < 0 || positions[1] <= positions[0] || positions[2] <= positions[1] {
		t.Fatalf("terminal scripts are missing or loaded out of order: %v", positions)
	}

	app, err := webAssets.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{"new Terminal(", "getWinSizeChars: true", "this.fitAddon.fit()"} {
		if !strings.Contains(string(app), contract) {
			t.Errorf("terminal integration is missing %q", contract)
		}
	}
	if strings.Contains(string(app), "— preset") {
		t.Error("port refresh must not re-add a missing serial port as a preset")
	}
}
