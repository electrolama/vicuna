//go:build darwin && !cgo

package main

import "errors"

func getDetailedPortsList() ([]detailedPortInfo, error) {
	return nil, errors.New("detailed serial port enumeration requires cgo on macOS")
}
