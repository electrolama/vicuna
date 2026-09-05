# Hardware modules

Hardware modules let Vicuña present serial modem-control lines using the language of a particular device. The serial transport remains generic; a module defines only how control lines are labelled, displayed, initialised, and changed.

## Baseline signal model

The built-in `generic-rs232` module exposes every modem-control signal supported by the serial backend:

| Signal | Direction from Vicuña | UI |
| --- | --- | --- |
| DTR | Output | HIGH/LOW button |
| RTS | Output | HIGH/LOW button |
| CTS | Input | HIGH/LOW indicator |
| DSR | Input | HIGH/LOW indicator |
| RI | Input | HIGH/LOW indicator |
| DCD | Input | HIGH/LOW indicator |

This generic view is useful for inspecting a device and confirming its electrical convention before assigning application-specific names.

## Why add a device module?

Modern USB-to-serial designs often reuse DTR, RTS, CTS, DSR, RI, or DCD for functions unrelated to a traditional modem. Common examples include reset and bootloader sequencing on ESP32 or STM32 boards, power switching, relay control, and status or fault inputs.

A device module turns those implementation details into controls an operator can understand. For example, a button can say `Boot` or `Target power` while still using DTR underneath.

## Module structure

Each compiled-in module has two small implementations:

1. **Go service adapter** — implements `hardwareModule`, declares its controls, and maps writable controls to `manager.SetSignals`.
2. **Browser adapter** — extends `HardwareModule`, renders the controls, supplies initial DTR/RTS states, and handles button toggles.

The generic implementation is in `hardware_generic.go` and `GenericRS232Module` in `web/app.js`. Device-specific Go implementations should use their own `hardware_<device>.go` file so the core registry stays easy to review.

### Service-side steps

1. Add a type implementing:

   ```go
   type hardwareModule interface {
       Definition() hardwareDefinition
       Set(manager managerAPI, control string, value bool) error
   }
   ```

2. Return a stable lowercase module ID, a user-facing label and description, and one definition per control.
3. Use `hardwareControlToggle` only for writable controls and `hardwareControlIndicator` for read-only states.
4. Map writable controls to DTR and/or RTS through `manager.SetSignals`.
5. Register the implementation in `availableHardwareModules` in `hardware.go`.
6. Add tests for the definition and every writable mapping.

The `/api/hardware` endpoint exposes registered definitions to the browser. `/api/hardware/control` routes a module control back to its service adapter.

### Browser-side steps

1. Add a class extending `HardwareModule` in `web/app.js`.
2. Implement `connectionSignals()` to return the initial `{ dtr, rts }` state for a new connection.
3. Implement `render()` using the shared `toggle(...)` and `indicator(...)` helpers.
4. Implement `toggleControl(control)` for writable controls. When connected, call `setControl`; when disconnected, update only browser settings.
5. Register one instance with `registerHardwareModule(...)`.

The browser intentionally ignores service modules without a matching browser adapter and logs a warning. This prevents a partially implemented module from exposing misleading controls.

## Safety rules

- Selecting a module must never toggle a line.
- Give power, reset, and boot controls conservative disconnected defaults.
- Keep the hardware selector locked for the duration of a connection.
- Treat `true` and `false` as logical modem-line states; document any inversion performed by the USB bridge or target circuit.
- Do not make an input signal writable in the UI.
- Use clear asserted and deasserted labels when HIGH/LOW would hide the device meaning.
- Test mappings without hardware by using a recording `managerAPI`, then verify the real circuit before deployment.

## Worked example: pt1

The included [pt1](https://lab.electrolama.com/project/pt1) module provides USB power control and fault reporting using two modem-control lines already handled by Vicuña:

| Device control | Modem signal | Behaviour |
| --- | --- | --- |
| VBUS | DTR output | Toggles the target USB power-switch enable; the PT1 enable is active-low. |
| Overcurrent | RI input | Displays the power-switch fault state. |

The service implementation is isolated in `hardware_pt1.go`. The matching `PT1Module` in `web/app.js` changes the labels to `ON`/`OFF` and `FAULT`/`CLEAR`, keeps VBUS off by default when first selected, and otherwise uses the same module APIs as any future device adapter.

Use this example as a template, replacing the device name, controls, mappings, labels, defaults, and tests with those required by the target hardware.
