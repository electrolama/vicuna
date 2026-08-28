# Vicuña

Vicuña is an extensible, browser-based serial monitor and terminal for embedded Linux consoles, microcontroller development, and general serial diagnostics. The Go server and web interface are embedded in one executable, with release builds for Linux, macOS (Intel and Apple silicon), and Windows.

## Highlights

- **Terminal** — xterm.js-powered VT/ECMA-48 emulation with ANSI colour, correct cursor and wide-character handling, alternate-screen support, paste, navigation keys, reflow, and scrollback.
- **Monitor** — timestamped RX/TX logging with visible control characters, optional ANSI colour, and export.
- **Hex** — binary-safe 16-byte rows with RX/TX offsets, timestamps, ASCII equivalents, and export.
- **Serial controls** — port discovery, common and custom baud rates, framing options, break, buffer reset, local echo, line endings, and text or hexadecimal sending.
- **Hardware modules** — reusable interpretations of DTR, RTS, CTS, DSR, RI, and DCD for reset, bootloader, power, and other device-specific controls.
- **Deployable by default** — JSON configuration, optional password protection, light and dark themes, and a loopback-only default listener.

## Quick start

Download the appropriate binary from a GitHub Release, or build from source:

```sh
go build -trimpath -o vicuna .
./vicuna
```

Open the address printed in the log. Vicuña listens on `127.0.0.1:8080` by default and controls serial ports attached to the host running the executable.

```text
-config <path>   Load a specific JSON configuration file
-listen <addr>   Set the HTTP listen address
-version         Print the build version and exit
```

## Hardware modules

`Generic RS232` is the built-in baseline and default. It exposes writable DTR and RTS lines as HIGH/LOW buttons and displays CTS, DSR, RI, and DCD as read-only indicators.

Hardware modules can give those lines device-specific names and behaviour without changing the serial transport. The included `pt1` adapter is a deliberately small worked example showing how to build such a module; Vicuña does not depend on pt1 hardware.

Hardware profiles and their controls live in the right sidebar so the top bar stays focused on choosing the serial port.

See [Hardware modules](docs/hardware-modules.md) for the extension contract and example.

## Configuration

Copy `vicuna.example.json` to `vicuna.json` and edit it for the target host. Configuration can select the serial port, baud rate, framing, operating mode, theme, hardware module, initial output states, and optional password protection.

```json
{
  "mode": "linux",
  "theme": "dark",
  "hardware": "generic-rs232",
  "password": "",
  "serial": {
    "port": "/dev/ttyUSB0",
    "baud": 115200,
    "dataBits": 8,
    "parity": "none",
    "stopBits": "1",
    "dtr": true,
    "rts": true
  }
}
```

Vicuña looks for `vicuna.json` in the working directory and beside the executable. An explicit `-config` path takes precedence. See [Configuration and security](docs/configuration.md) for every field and the network-access guidance.

## Documentation

- [Documentation index](docs/README.md)
- [Configuration and security](docs/configuration.md)
- [Deployment](docs/deployment.md)
- [Hardware modules](docs/hardware-modules.md)
- [Release process](docs/releases.md)
- [Architecture notes](docs/architecture.md)

## Development

Go 1.27 or newer is required:

```sh
go test ./...
go vet ./...
go build -trimpath -o vicuna .
```

Compiled files are intentionally excluded from source control. GitHub Actions runs the full verification suite on pushes and pull requests, while version tags create checksummed release binaries. See the [release process](docs/releases.md) for details.

## AI contribution

AI-assisted development tools made meaningful contributions to Vicuña's implementation, tests, interface refinement, and documentation. Project direction, review, and release decisions remain the responsibility of the human maintainers.

## Licence

Vicuña is available under the [MIT License](LICENSE). Third-party dependency licences are embedded in the executable and served at `/third-party-notices.txt`.
