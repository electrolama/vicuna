package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDeploymentConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vicuna.json")
	data := []byte(`{
  "mode": "embedded",
  "theme": "light",
  "hardware": "generic-rs232",
  "password": "test-secret",
  "serial": {
    "port": " /dev/ttyUSB0 ",
    "baud": 921600,
    "dataBits": 8,
    "parity": "even",
    "stopBits": "2",
    "dtr": false,
    "rts": false
  }
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	config, loadedPath, err := loadDeploymentConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Configured || loadedPath != path {
		t.Fatalf("configuration was not marked as loaded: %+v %q", config, loadedPath)
	}
	if config.Mode != "embedded" || config.Theme != "light" || config.Hardware != "generic-rs232" || config.Password != "test-secret" || config.Serial.Port != "/dev/ttyUSB0" || config.Serial.Baud != 921600 {
		t.Fatalf("unexpected configuration: %+v", config)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "test-secret") || strings.Contains(string(encoded), "password") {
		t.Fatalf("password leaked through client configuration: %s", encoded)
	}
}

func TestLoadDeploymentConfigRejectsInvalidValues(t *testing.T) {
	tests := []string{
		`{"mode":"desktop"}`,
		`{"theme":"sepia"}`,
		`{"hardware":"unknown"}`,
		`{"serial":{"baud":1}}`,
		`{"unexpected":true}`,
	}
	for _, data := range tests {
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadDeploymentConfig(path); err == nil {
			t.Fatalf("expected invalid configuration to fail: %s", data)
		}
	}
}

func TestMissingAutomaticConfigUsesDefaults(t *testing.T) {
	config := defaultDeploymentConfig()
	if config.Configured || config.Mode != "linux" || config.Theme != "dark" || config.Hardware != "generic-rs232" || config.Serial.Baud != 115200 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
}
