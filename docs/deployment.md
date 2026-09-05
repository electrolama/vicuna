# Deployment

## Build from source

Go 1.27 or newer is required:

```sh
go test ./...
go vet ./...
go build -trimpath -ldflags="-s -w" -o vicuna .
```

Cross-compile common deployment targets with `CGO_ENABLED=0`:

```sh
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/vicuna-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/vicuna-linux-arm64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o dist/vicuna-linux-armv7 .
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -o dist/vicuna-darwin-amd64 .
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o dist/vicuna-darwin-arm64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-H windowsgui" -o dist/vicuna-windows-amd64.exe .
```

Compiled files and `dist/` are ignored by Git. Normal releases should use the automated process described in [releases.md](releases.md).

## Windows

Double-click the Windows release executable. It starts the local server, adds a blue terminal icon to the system tray, and opens the interface in your default browser. Windows may place the icon in the tray's hidden-icons menu; you can drag it onto the taskbar's tray area.

Click the icon to open the interface. Its right-click menu provides **Open Vicuña**, **Open logs**, and **Quit**. Closing a browser tab leaves the server and serial connection running. **Quit** closes browser event streams, finishes active requests, and releases the serial port. The icon is restored if Windows Explorer restarts.

Launching Vicuna again with the same `-listen` value activates the existing tray instance for the current Windows user/session. The first instance's configuration stays in effect; quit it before changing configuration. Different listen addresses can run independently. A port occupied by another application produces a startup error dialog.

Tray-mode diagnostics are written to `%LOCALAPPDATA%\Vicuna\logs\vicuna.log`. Non-default listen addresses use a filename with an address-derived suffix. **Open logs** opens the current file. At startup, a log of 5 MiB or larger is rotated to `.log.1`, replacing the previous backup. Logs contain application diagnostics, not serial traffic; export serial data from the browser interface.

For troubleshooting or a session without a desktop, use:

```powershell
.\vicuna.exe -console
.\vicuna.exe -console -listen 127.0.0.1:9090 -config C:\path\vicuna.json
```

Console mode attaches to the invoking terminal, or creates a console if necessary. It writes diagnostics to stderr, does not open the browser or tray, and stops with Ctrl+C. It starts its own server, so quit a tray instance using the same port first. `-help` and `-version` also work with terminal output and redirection; when launched without an output destination, they display a dialog. In PowerShell, pipe GUI-executable output to `Out-String` when you need to wait for it, for example `.\vicuna.exe -version | Out-String`.

Build the tray executable locally in PowerShell with:

```powershell
$env:CGO_ENABLED = '0'
go build -trimpath -ldflags="-s -w -H windowsgui" -o vicuna.exe .
```

Linux and macOS continue to run in the terminal as before.

## macOS

Use `vicuna-darwin-arm64` on Apple silicon Macs and `vicuna-darwin-amd64` on Intel Macs. After downloading the appropriate release artifact and checking its SHA-256 hash against `SHA256SUMS.txt`:

```sh
chmod +x vicuna-darwin-arm64
./vicuna-darwin-arm64
```

Replace the filename with `vicuna-darwin-amd64` on an Intel Mac. Serial devices normally appear as `/dev/cu.*`; select the required device in Vicuña after it starts. The portable macOS builds list device names but omit the optional USB product and VID/PID labels because those require a native cgo build against Apple's IOKit framework.

The release binaries are not currently code-signed or notarized. macOS may therefore ask you to confirm the first launch in **System Settings > Privacy & Security**. Only approve a binary after verifying its checksum.

## Linux and Raspberry Pi service

The included `vicuna.service` runs Vicuña as an unprivileged user, grants serial-device access through the supplementary `dialout` group, and retains the safe loopback listener.

After downloading the correct release binary and verifying its checksum:

```sh
sudo useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin vicuna
sudo install -m 0755 vicuna-linux-arm64 /usr/local/bin/vicuna
sudo install -d -o root -g vicuna -m 0750 /etc/vicuna
sudo install -o root -g vicuna -m 0640 vicuna.json /etc/vicuna/vicuna.json
sudo install -m 0644 vicuna.service /etc/systemd/system/vicuna.service
sudo systemctl daemon-reload
sudo systemctl enable --now vicuna
```

Select the amd64 or ARMv7 binary instead where appropriate.

To expose the service to another machine, create a systemd override:

```sh
sudo systemctl edit vicuna
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/vicuna -config /etc/vicuna/vicuna.json -listen 0.0.0.0:8080
```

Restart with `sudo systemctl restart vicuna`. Configure authentication and TLS as described in [configuration.md](configuration.md) before exposing the listener.

For manual execution, the user account usually needs membership in `dialout`:

```sh
sudo usermod -aG dialout "$USER"
```

Log out and back in after changing group membership.
