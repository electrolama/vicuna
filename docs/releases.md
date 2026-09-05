# Release process

Vicuña releases are produced by GitHub Actions. Compiled binaries are never committed to the repository.

## Continuous integration

`.github/workflows/ci.yml` runs for every push and pull request. It checks:

- Go formatting;
- module checksums;
- `go vet`;
- the Go test suite with the race detector;
- known vulnerabilities with `govulncheck`;
- browser JavaScript syntax;
- the example JSON configuration; and
- a versioned smoke build.

A separate Windows job runs `go vet`, the test suite (including native tray/instance tests), and a GUI-subsystem build with redirected `-version` and `-help` smoke checks. The live tray integration test also runs when an Explorer desktop is available; it skips on headless runners.

A change should not be released until CI succeeds on the intended commit. Both CI and tag builds call `.github/workflows/verify.yml`; release build and publish jobs depend on that shared verification workflow, so pushing a tag cannot bypass testing even if the separate CI run has not completed.

## Versioning

Releases use tags beginning with `v`, for example `v1.2.0`. The leading `v` identifies the Git tag; the embedded application version is `1.2.0`.

Use semantic versioning where practical:

- increment the major version for incompatible configuration, API, or module-contract changes;
- increment the minor version for backwards-compatible features; and
- increment the patch version for backwards-compatible fixes.

## Publishing a release

1. Update user-facing documentation and the example configuration where required.
2. Ensure CI is green on `main`.
3. Review the pending commit and confirm that no binaries, credentials, or local configuration files are present.
4. Create and push an annotated version tag:

   ```sh
   git switch main
   git pull --ff-only
   git tag -a v1.0.0 -m "Vicuña 1.0.0"
   git push origin v1.0.0
   ```

5. The `Release builds` workflow validates the tag, reruns the full verification suite, cross-compiles, and publishes the release only after every check passes. Review the generated release notes and edit them if needed.

The workflow builds:

| Artifact | Target |
| --- | --- |
| `vicuna-<version>-linux-amd64` | 64-bit Intel/AMD Linux |
| `vicuna-<version>-linux-arm64` | 64-bit ARM Linux and Raspberry Pi |
| `vicuna-<version>-linux-armv7` | 32-bit ARMv7 Linux and Raspberry Pi |
| `vicuna-<version>-darwin-amd64` | Intel Mac |
| `vicuna-<version>-darwin-arm64` | Apple silicon Mac |
| `vicuna-<version>-windows-amd64.exe` | 64-bit Windows |
| `LICENSE` | Vicuña's MIT licence |
| `THIRD_PARTY_NOTICES.txt` | Notices and licence texts for bundled dependencies |
| `SHA256SUMS.txt` | SHA-256 hashes for every binary and licence file |

The Go linker embeds the version derived from the tag. Builds use `CGO_ENABLED=0`, `-trimpath`, and stripped symbols for portable standalone executables.

Windows releases additionally use `-H windowsgui` so normal launches show a system tray icon without a console window. The same executable provides `-console` for terminal use. Tray integration uses native Windows APIs and adds no external runtime requirement.

## Manual release build

The release workflow can also be started with **Run workflow** in GitHub Actions. Supply the version to embed. A manual run creates a combined downloadable Actions artifact but does not create a GitHub Release or tag.

Use manual runs for release candidates and build verification. Public releases should come from an immutable pushed tag.

## Verification

After downloading a release, verify it against `SHA256SUMS.txt`:

```sh
sha256sum -c SHA256SUMS.txt
./vicuna-0.4.0-linux-arm64 -version
```

On macOS, use `shasum -a 256`; on Windows, use `Get-FileHash -Algorithm SHA256`. Compare the result with the published checksum file.
