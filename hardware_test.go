package main

import "testing"

func TestRegisteredHardwareDefinitionsAreValid(t *testing.T) {
	moduleIDs := make(map[string]bool)
	for _, module := range availableHardwareModules() {
		definition := module.Definition()
		if definition.ID == "" || definition.Label == "" || definition.Description == "" {
			t.Fatalf("module metadata is incomplete: %+v", definition)
		}
		if moduleIDs[definition.ID] {
			t.Fatalf("duplicate hardware module ID: %s", definition.ID)
		}
		moduleIDs[definition.ID] = true

		controlIDs := make(map[string]bool)
		for _, control := range definition.Controls {
			if control.ID == "" || control.Label == "" || control.Signal == "" {
				t.Fatalf("module %s has incomplete control metadata: %+v", definition.ID, control)
			}
			if control.Kind != hardwareControlToggle && control.Kind != hardwareControlIndicator {
				t.Fatalf("module %s control %s has invalid kind %q", definition.ID, control.ID, control.Kind)
			}
			if controlIDs[control.ID] {
				t.Fatalf("module %s has duplicate control ID %q", definition.ID, control.ID)
			}
			controlIDs[control.ID] = true
		}
	}
}

func TestGenericRS232Definition(t *testing.T) {
	definition := (genericRS232Module{}).Definition()
	want := []struct{ id, kind, signal string }{
		{"dtr", hardwareControlToggle, "dtr"},
		{"rts", hardwareControlToggle, "rts"},
		{"cts", hardwareControlIndicator, "cts"},
		{"dsr", hardwareControlIndicator, "dsr"},
		{"ri", hardwareControlIndicator, "ri"},
		{"dcd", hardwareControlIndicator, "dcd"},
	}
	if definition.ID != "generic-rs232" || len(definition.Controls) != len(want) {
		t.Fatalf("unexpected generic RS232 definition: %+v", definition)
	}
	for index, expected := range want {
		control := definition.Controls[index]
		if control.ID != expected.id || control.Kind != expected.kind || control.Signal != expected.signal {
			t.Fatalf("control %d: got %+v, want %+v", index, control, expected)
		}
	}
}

func TestGenericRS232OutputsMapDirectlyToSignals(t *testing.T) {
	manager := &fakeManager{}
	module := genericRS232Module{}
	if err := module.Set(manager, "dtr", true); err != nil {
		t.Fatal(err)
	}
	if manager.dtr == nil || !*manager.dtr || manager.rts != nil {
		t.Fatalf("DTR mapping changed unexpected signals: dtr=%v rts=%v", manager.dtr, manager.rts)
	}
	if err := module.Set(manager, "rts", false); err != nil {
		t.Fatal(err)
	}
	if manager.rts == nil || *manager.rts {
		t.Fatalf("RTS mapping was not applied: %v", manager.rts)
	}
}

func TestPT1ExampleDefinitionAndVBUSMapping(t *testing.T) {
	module := pt1Module{}
	definition := module.Definition()
	if definition.ID != "pt1" || len(definition.Controls) != 2 ||
		definition.Controls[0].ID != "vbus" || definition.Controls[0].Signal != "dtr" ||
		definition.Controls[1].ID != "overcurrent" || definition.Controls[1].Signal != "ri" {
		t.Fatalf("unexpected pt1 example definition: %+v", definition)
	}

	manager := &fakeManager{}
	if err := module.Set(manager, "vbus", true); err != nil {
		t.Fatal(err)
	}
	if manager.dtr == nil || !*manager.dtr || manager.rts != nil {
		t.Fatalf("pt1 VBUS should set DTR only: dtr=%v rts=%v", manager.dtr, manager.rts)
	}
}
