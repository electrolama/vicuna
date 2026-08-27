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
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o dist/vicuna-windows-amd64.exe .
```

Compiled files and `dist/` are ignored by Git. Normal releases should use the automated process described in [releases.md](releases.md).

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
