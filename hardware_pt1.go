package main

import "errors"

// pt1Module is an example of a device-specific adapter. It gives the raw DTR
// and RI lines names that match the pt1 hardware instead of changing the
// serial transport itself.
type pt1Module struct{}

func (pt1Module) Definition() hardwareDefinition {
	return hardwareDefinition{
		ID:          "pt1",
		Label:       "pt1",
		Description: "Example device module mapping VBUS and overcurrent to modem-control lines.",
		Controls: []hardwareControlDefinition{
			{ID: "vbus", Label: "VBUS", Kind: hardwareControlToggle, Signal: "dtr", Description: "DTR drives the active-high USB power-switch enable."},
			{ID: "overcurrent", Label: "Overcurrent", Kind: hardwareControlIndicator, Signal: "ri", Description: "RI reports the USB power-switch fault output."},
		},
	}
}

func (pt1Module) Set(manager managerAPI, control string, value bool) error {
	if control != "vbus" {
		return errors.New("pt1 control must be vbus")
	}
	return manager.SetSignals(&value, nil)
}
