# Ullage

A Windows-only, privacy-first disk space analyzer for developers. It scans drives locally,
serves its UI on `127.0.0.1`, and uses AI only through manual copy and paste. The program
itself never makes an outbound network connection.

Status: under construction, phase by phase. See [ARCHITECTURE.md](ARCHITECTURE.md).

## Building

Requirements: Go 1.24+, Node.js 22+ (build time only).

```sh
cd web && npm ci && npm run build && cd ..
go build -trimpath -ldflags "-H windowsgui" -o ullage.exe ./cmd/ullage
```

Cross-compiling from Linux or macOS works the same way with `GOOS=windows`.

## Testing

```sh
cd web && npm test && cd ..
go test ./...          # includes tools/nonetwork, the no-outbound-network build check
```

The Win32 fixture tests (junctions, NTFS compression, sparse files, ACL-denied folders)
run only on Windows; CI runs them on `windows-latest`.
