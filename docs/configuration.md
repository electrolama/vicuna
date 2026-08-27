# Configuration and security

Vicuña accepts deployment defaults from JSON while retaining interactive preferences in the browser. Copy `vicuna.example.json` to `vicuna.json` and adjust it for the host.

## Fields

| Field | Values and behaviour |
| --- | --- |
| `mode` | `linux` opens in Terminal with ANSI colour; `embedded` opens in the timestamped Monitor view. |
| `theme` | `dark` or `light`; both can also be selected in the interface. |
| `hardware` | `generic-rs232` by default, or the ID of another compiled-in hardware module. |
| `password` | Empty disables authentication; any other value enables HTTP Basic Authentication with username `vicuna`. |
| `serial.port` | Host serial-device name, such as `/dev/ttyUSB0` or `COM7`. |
| `serial.baud` | Integer from 50 to 12,000,000. |
| `serial.dataBits` | 5, 6, 7, or 8. |
| `serial.parity` | `none`, `odd`, `even`, `mark`, or `space`. |
| `serial.stopBits` | `1`, `1.5`, or `2`. |
| `serial.dtr`, `serial.rts` | Initial logical output states applied when connecting. |

Configured values take precedence over browser-local preferences whenever the page loads. They preselect the interface but do not connect automatically. Unknown fields and invalid values stop startup with an explanatory error.

## Configuration discovery

Vicuña checks for `vicuna.json` in the working directory and then beside the executable. Select another file explicitly with:

```sh
./vicuna -config /etc/vicuna/vicuna.json
```

Keep the configuration readable only by the service account when it contains a password.

## Network access

The default `127.0.0.1:8080` listener is reachable only from the local host. To listen on every interface:

```sh
./vicuna -config vicuna.json -listen 0.0.0.0:8080
```

Set a non-empty password before exposing the service. HTTP Basic Authentication restricts access to the page, API, and event stream, but does not encrypt credentials or serial data. Use an HTTPS reverse proxy or another TLS endpoint whenever traffic crosses an untrusted network.

Vicuña controls physical serial ports and their output lines. Restrict network and filesystem access accordingly; a user who can access Vicuña can interact with the connected target hardware.
