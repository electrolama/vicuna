package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const defaultConfigFilename = "vicuna.json"

type deploymentConfig struct {
	Configured bool         `json:"configured"`
	Mode       string       `json:"mode"`
	Theme      string       `json:"theme"`
	Hardware   string       `json:"hardware"`
	Serial     serialConfig `json:"serial"`
	Password   string       `json:"-"`
}

type configFile struct {
	Mode     string       `json:"mode"`
	Theme    string       `json:"theme"`
	Hardware string       `json:"hardware"`
	Serial   serialConfig `json:"serial"`
	Password string       `json:"password"`
}

func defaultDeploymentConfig() deploymentConfig {
	return deploymentConfig{
		Mode:     "linux",
		Theme:    "dark",
		Hardware: "generic-rs232",
		Serial: serialConfig{
			Baud:     115200,
			DataBits: 8,
			Parity:   "none",
			StopBits: "1",
			DTR:      true,
			RTS:      true,
		},
	}
}

func loadDeploymentConfig(requestedPath string) (deploymentConfig, string, error) {
	config := defaultDeploymentConfig()
	path, found, err := findConfigPath(requestedPath)
	if err != nil {
		return config, "", err
	}
	if !found {
		return config, "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return config, "", fmt.Errorf("read config %s: %w", path, err)
	}
	if len(data) > 1<<20 {
		return config, "", errors.New("configuration file is larger than 1 MiB")
	}

	file := configFile{Mode: config.Mode, Theme: config.Theme, Hardware: config.Hardware, Serial: config.Serial}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return config, "", fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return config, "", fmt.Errorf("parse config %s: expected one JSON object", path)
	}

	config.Configured = true
	config.Mode = strings.ToLower(strings.TrimSpace(file.Mode))
	config.Theme = strings.ToLower(strings.TrimSpace(file.Theme))
	config.Hardware = strings.ToLower(strings.TrimSpace(file.Hardware))
	config.Serial = file.Serial
	config.Password = file.Password
	config.Serial.Port = strings.TrimSpace(config.Serial.Port)
	if err := validateDeploymentConfig(&config); err != nil {
		return defaultDeploymentConfig(), "", fmt.Errorf("validate config %s: %w", path, err)
	}
	return config, path, nil
}

func findConfigPath(requestedPath string) (string, bool, error) {
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath != "" {
		path, err := filepath.Abs(requestedPath)
		if err != nil {
			return "", false, fmt.Errorf("resolve config path: %w", err)
		}
		if _, err := os.Stat(path); err != nil {
			return "", false, fmt.Errorf("open requested config %s: %w", path, err)
		}
		return path, true, nil
	}

	candidates := make([]string, 0, 2)
	if workingDirectory, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(workingDirectory, defaultConfigFilename))
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), defaultConfigFilename)
		if len(candidates) == 0 || !samePath(candidate, candidates[0]) {
			candidates = append(candidates, candidate)
		}
	}
	for _, candidate := range candidates {
		_, err := os.Stat(candidate)
		switch {
		case err == nil:
			path, resolveErr := filepath.Abs(candidate)
			return path, true, resolveErr
		case os.IsNotExist(err):
			continue
		default:
			return "", false, fmt.Errorf("inspect config %s: %w", candidate, err)
		}
	}
	return "", false, nil
}

func validateDeploymentConfig(config *deploymentConfig) error {
	switch config.Mode {
	case "linux", "embedded":
	default:
		return errors.New("mode must be linux or embedded")
	}
	switch config.Theme {
	case "dark", "light":
	default:
		return errors.New("theme must be dark or light")
	}
	if !knownHardwareModule(config.Hardware) {
		return errors.New("hardware does not name an available module")
	}

	validated := config.Serial
	if validated.Port == "" {
		validated.Port = "configured-port"
	}
	if _, err := modeFromConfig(&validated); err != nil {
		return fmt.Errorf("serial: %w", err)
	}
	config.Serial.Parity = validated.Parity
	config.Serial.StopBits = validated.StopBits
	return nil
}

func samePath(left, right string) bool {
	left, leftErr := filepath.Abs(left)
	right, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
