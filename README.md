# Ullage

A privacy-first disk space analyzer for Windows, for any kind of user. It scans your drives
locally, shows its interface in your browser on `127.0.0.1`, and uses AI only through
copy and paste: you decide what text leaves your PC, and the program itself never makes
an outbound network connection.

(*Ullage* is the empty space at the top of a cask or bottle, the part that is not
filled.)

## How it works

```mermaid
flowchart LR
    A[1 Scan] --> B[2 Overview]
    B --> C[3 Redaction]
    C --> D[4 Export prompt]
    D -- you copy --> AI[(Any chat assistant)]
    AI -- you paste --> E[5 Paste response]
    E --> F[6 Results]
    F --> G[7 Delete]
```

1. **Scan** drives or folders. Sizes are what files really occupy on disk (compression,
   sparse files, hard links counted once, cloud placeholders counted as not stored here).
2. **Overview**: every byte is in one tier. *System* (Windows, page file...) is never
   touched; *Known* items are recognized by built-in recipes (package caches, browser
   caches, Docker and WSL disks...); everything else is *Unknown*.
3. **Redaction**: choose what the assistant may not see. Your user name, machine name
   and profile path are always hidden. Rules are stored encrypted with Windows DPAPI.
4. **Export**: Ullage writes a compact, redacted list of the large Unknown items with
   numeric IDs. You copy it into any assistant (Claude, ChatGPT, ...).
5. **Paste** the reply. Ullage validates it strictly: the reply can only name IDs from
   the prompt and choose from fixed values. Anything else is rejected, with a repair
   prompt to send back.
6. **Results**: recipe and AI categories, largest first, with real names shown again
   locally. Ask for explanations of AI categories; they go out as a short second prompt.
7. **Delete**: every item is checked against the disk again right before deletion.
   Recycle Bin by default; permanent deletion needs you to type a confirmation.

## Safety and privacy

- **No outbound network.** No telemetry, update checks or remote assets. A build check
  rejects networking code, a runtime guard disables Go's HTTP client and DNS, and the
  page's Content Security Policy blocks remote loads.
- **Local server hardening.** It listens on `127.0.0.1` only, on a random port, behind a
  per-launch token. Requests with the wrong Host or Origin are refused, and state
  changes need POST with the token in a header.
- **The AI never authors actions.** Replies are untrusted input. Ullage builds every
  deletion itself, from IDs that exist in the current scan. AI cleanup advice is shown
  as text and is never executed.
- **Protected locations.** Ullage never deletes in the System tier, under `C:\Windows`
  or Program Files, outside the scanned folders, or any profile or folder containing
  one.
- **Changes block deletion.** Any change since the scan blocks an item: size, files,
  modification time, or a folder replaced by a link.
- **Fail closed.** If the redaction rules cannot be read, no prompt is generated.
- **Cloud-synced items** (OneDrive and similar) can be deleted, but Ullage warns that this
  also deletes the cloud copy.

## Install and run

1. Download `ullage.exe` (one file, no installer) and put it where you keep programs,
   for example `%LOCALAPPDATA%\Programs\Ullage\`.
2. Run it. Your browser opens on a local, one-time link. The first run adds
   **Ullage** to the Start menu.
3. Running it again while it is open just opens a new tab of the same session. It exits
   by itself 15 minutes after the last tab closes.

To read folders that need administrator rights, use *Relaunch as administrator* on the
Scan screen.

Command line options: `--no-browser` prints the link instead of opening it;
`--install-shortcut` and `--remove-shortcut` manage the Start menu entry.

### Where Ullage keeps data

| What | Where |
|------|-------|
| Settings | `%APPDATA%\Ullage\config.json` |
| Redaction rules (encrypted, DPAPI) | `%APPDATA%\Ullage\rules.dpapi` |
| History: dates, drives and sizes, no paths (encrypted, DPAPI) | `%LOCALAPPDATA%\Ullage\history.dpapi` |
| Start menu shortcut | `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Ullage.lnk` |

To uninstall, run `ullage.exe --remove-shortcut`, delete the executable, and delete the
two `Ullage` folders above.

## Building

Requirements: Go 1.24+ and Node.js 22+ (build time only). The result is one
self-contained `bin\ullage.exe`.

```sh
scripts/build.sh            # Linux, macOS, Git Bash
```

```powershell
scripts\build.ps1           # PowerShell
```

## Testing

```sh
cd web && npm ci && npm test && cd ..
go test ./...    # includes the no-outbound-network build check and an API end-to-end test
```

The Windows-only tests cover:

- junctions, NTFS compression, sparse files and ACL-denied folders;
- DPAPI;
- the real Recycle Bin;
- the single-instance pipe;
- the Start menu shortcut.

They run on `windows-latest` in CI. `go run ./tools/tokenestimate` estimates prompt sizes
offline; it is a development tool and not part of the app.

## Documentation

[Architecture](docs/ARCHITECTURE.md), [requirements](docs/requirements.md),
[decisions](docs/decisions.md), [design conflicts](docs/design-conflicts.md) and the
[phase plan](docs/phases.md).
