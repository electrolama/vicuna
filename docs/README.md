# Vicuña documentation

This directory contains the operational and extension documentation for Vicuña.

- [Configuration and security](configuration.md) — deployment defaults, serial settings, authentication, and network exposure.
- [Deployment](deployment.md) — local builds, cross-compilation, Linux installation, and systemd operation.
- [Hardware modules](hardware-modules.md) — the control-signal model and how to add a device-specific module.
- [Release process](releases.md) — automated checks, versioning, release artifacts, and publication procedure.
- [Architecture notes](architecture.md) — terminal modes, byte transport, and concurrency choices.

User-facing behaviour should be introduced briefly in the repository README and explained in detail here. Keep device-specific material in the hardware-module guide so the core project remains hardware-neutral.
