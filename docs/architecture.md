# Architecture notes

## Views and byte handling

Terminal, Monitor, and Hex are separate views over the same bidirectional serial byte stream.

Terminal uses xterm.js for production-grade VT/ECMA-48 parsing, cursor state, alternate-screen buffers, Unicode cell widths, and viewport reflow. Monitor is intended for chronological firmware logs and can add timestamps without corrupting terminal cursor or screen-erasure sequences. Hex remains binary-safe for arbitrary protocols.

The fit addon resizes the browser-side terminal whenever its container changes. A raw UART has no PTY control channel, so Vicuña cannot deliver `TIOCSWINSZ` or `SIGWINCH` to a program on the remote machine. Vicuña responds to xterm character-size queries, allowing the remote `resize` utility to discover the current geometry, and shows an equivalent `stty rows … cols …` command in the sidebar. Full-screen programs should be started after one of those commands when the browser size has changed.

Serial data travels between the Go process and browser as base64 inside a same-origin server-sent-event stream. This preserves zero bytes and invalid UTF-8. Sends use short same-origin HTTP requests.

## Concurrency

The serial reader owns incoming device traffic and publishes it through the event hub. Slow browser clients are dropped from the event path rather than blocking serial input. Connection state is rechecked around modem-status operations so a delayed status query cannot report a stale connection after disconnect.

## Embedded frontend

The HTML, CSS, JavaScript, and third-party notices under `web/` are embedded into the Go executable. A deployment therefore needs only the binary and, optionally, a JSON configuration file.

## Hardware separation

The serial transport models standard output and input signals. Hardware modules provide presentation and device semantics on top of those signals. See [hardware-modules.md](hardware-modules.md) for the extension boundary.
