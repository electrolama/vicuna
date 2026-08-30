# Architecture notes

## Views and byte handling

Terminal, Monitor, and Hex are separate views over the same bidirectional serial byte stream.

Terminal uses xterm.js for production-grade VT/ECMA-48 parsing, cursor state, alternate-screen buffers, Unicode cell widths, and viewport reflow. Monitor is intended for chronological firmware logs and can add timestamps without corrupting terminal cursor or screen-erasure sequences. Hex remains binary-safe for arbitrary protocols.

The fit addon resizes the browser-side terminal whenever its container changes. A raw UART has no PTY control channel, so Vicuña cannot deliver `TIOCSWINSZ` or `SIGWINCH` to a program on the remote machine. Vicuña responds to xterm character-size queries, allowing the remote `resize` utility to discover the current geometry, and shows an equivalent `stty rows … cols …` command in the sidebar. Full-screen programs should be started after one of those commands when the browser size has changed.

Serial data travels between the Go process and browser as base64 inside a same-origin server-sent-event stream. This preserves zero bytes and invalid UTF-8. Sends use short same-origin HTTP requests.

## Concurrency

The serial reader owns incoming device traffic and publishes it through the event hub. Slow browser clients are dropped from the event path rather than blocking serial input. Connection state is rechecked around modem-status operations so a delayed status query cannot report a stale connection after disconnect.

While connected, a separate presence check compares the active port with the operating system's current device list. If the device disappears, Vicuña closes the stale handle and publishes a disconnected status even when a serial driver only returns read timeouts after unplugging. A failed enumeration is treated as transient and does not disconnect the device by itself. The browser also refreshes the available-port selector periodically while it is open.

If the browser loses its event stream or an API request cannot reach the backend, it replaces the application with a backend-unavailable screen and disables the stale controls. The event stream reconnects automatically; the application returns only after receiving a fresh connection status and then refreshes the port list.

## Embedded frontend

The HTML, CSS, JavaScript, and third-party notices under `web/` are embedded into the Go executable. A deployment therefore needs only the binary and, optionally, a JSON configuration file.

## Hardware separation

The serial transport models standard output and input signals. Hardware modules provide presentation and device semantics on top of those signals. See [hardware-modules.md](hardware-modules.md) for the extension boundary.
