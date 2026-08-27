package main

import "errors"

// genericRS232Module is Vicuña's baseline hardware module. It exposes the
// modem-control signals without assigning them device-specific meanings.
type genericRS232Module struct{}

func (genericRS232Module) Definition() hardwareDefinition {
	return hardwareDefinition{
		ID:          "generic-rs232",
		Label:       "Generic RS232",
		Description: "Direct access to standard RS232 modem-control signals.",
		Controls: []hardwareControlDefinition{
			{ID: "dtr", Label: "DTR", Kind: hardwareControlToggle, Signal: "dtr"},
			{ID: "rts", Label: "RTS", Kind: hardwareControlToggle, Signal: "rts"},
			{ID: "cts", Label: "CTS", Kind: hardwareControlIndicator, Signal: "cts"},
			{ID: "dsr", Label: "DSR", Kind: hardwareControlIndicator, Signal: "dsr"},
			{ID: "ri", Label: "RI", Kind: hardwareControlIndicator, Signal: "ri"},
			{ID: "dcd", Label: "DCD", Kind: hardwareControlIndicator, Signal: "dcd"},
		},
	}
}

func (genericRS232Module) Set(manager managerAPI, control string, value bool) error {
	switch control {
	case "dtr":
		return manager.SetSignals(&value, nil)
	case "rts":
		return manager.SetSignals(nil, &value)
	default:
		return errors.New("generic RS232 control must be dtr or rts")
	}
}
