package main

import "testing"

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
