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
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o dist/vicuna-windows-amd64.exe .
```

Compiled files and `dist/` are ignored by Git. Normal releases should use the automated process described in [releases.md](releases.md).

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
