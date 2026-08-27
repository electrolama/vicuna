package main

import (
	"errors"
	"sort"
)

type hardwareControlDefinition struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Signal      string `json:"signal"`
	Description string `json:"description,omitempty"`
}

const (
	hardwareControlToggle    = "toggle"
	hardwareControlIndicator = "indicator"
)

type hardwareDefinition struct {
	ID          string                      `json:"id"`
	Label       string                      `json:"label"`
	Description string                      `json:"description"`
	Controls    []hardwareControlDefinition `json:"controls"`
}

type hardwareModule interface {
	Definition() hardwareDefinition
	Set(manager managerAPI, control string, value bool) error
}

type hardwareRegistry struct {
	manager managerAPI
	modules map[string]hardwareModule
}

func newHardwareRegistry(manager managerAPI) *hardwareRegistry {
	modules := availableHardwareModules()
	registry := &hardwareRegistry{manager: manager, modules: make(map[string]hardwareModule, len(modules))}
	for _, module := range modules {
		registry.modules[module.Definition().ID] = module
	}
	return registry
}

func availableHardwareModules() []hardwareModule {
	// Generic RS232 is the baseline module. Device-specific adapters are
	// compiled in by registering another implementation here.
	return []hardwareModule{genericRS232Module{}, pt1Module{}}
}

func knownHardwareModule(id string) bool {
	for _, module := range availableHardwareModules() {
		if module.Definition().ID == id {
			return true
		}
	}
	return false
}

func (r *hardwareRegistry) Definitions() []hardwareDefinition {
	definitions := make([]hardwareDefinition, 0, len(r.modules))
	for _, module := range r.modules {
		definitions = append(definitions, module.Definition())
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions
}

func (r *hardwareRegistry) Set(moduleID, control string, value bool) error {
	module, ok := r.modules[moduleID]
	if !ok {
		return errors.New("unknown hardware module")
	}
	return module.Set(r.manager, control, value)
}
