//go:build !darwin || cgo

package main

import "go.bug.st/serial/enumerator"

func getDetailedPortsList() ([]detailedPortInfo, error) {
	details, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}

	ports := make([]detailedPortInfo, 0, len(details))
	for _, detail := range details {
		ports = append(ports, detailedPortInfo{
			Name:         detail.Name,
			USB:          detail.IsUSB,
			VID:          detail.VID,
			PID:          detail.PID,
			SerialNumber: detail.SerialNumber,
			Product:      detail.Product,
		})
	}
	return ports, nil
}
