# Decision log

Each decision records what was decided, why, and its status. "Approved" means the project
owner explicitly approved it; "Reported" means it was made during a phase and reported in
that phase's summary for review. Newer decisions can supersede older ones; superseded
entries stay for the record.

## Stack and architecture

### D-001: Go backend, pure Go, no cgo — Approved (Phase 0)
Single static `ullage.exe` with no runtime install; `golang.org/x/sys/windows` covers the
Win32 surface; goroutines suit parallel directory enumeration; cross-compiles from Linux.
Rejected: Rust (narrowly; no Windows cross-toolchain in the dev container, slower test
loop, scanner is I/O bound), C#/.NET Native AOT (large, Windows-only builds), C++ (manual
memory safety while parsing untrusted AI text), Python (too slow), Electron/Tauri (bundled
or extra runtimes, not "any browser").

### D-002: Preact + TypeScript + Vite frontend, embedded with go:embed — Approved (Phase 0)
Same component model as the design's React code at a fraction of the size. Node is a
build-time dependency only. Dependencies are pinned to the stable major lines (Preact 10,
Vite 6, TypeScript 5, Vitest 3) rather than the newest majors, which were too new to vouch
for.

### D-003: Fonts bundled, strict Content-Security-Policy — Approved (Phase 0)
IBM Plex Sans and Mono (latin and latin-ext `.woff2`, OFL) are committed in
`web/src/fonts`. The CSP (`default-src 'self'`, no inline styles) means even an
accidental remote URL cannot load. Inline styles are set through the CSSOM, which the CSP
allows.

### D-009: One platform boundary for a future Linux port — Approved (amendment A-1)
All OS-specific code lives in `internal/platform` behind build tags; core packages use
portable types. A working Linux implementation exists from Phase 1 and backs the
real-filesystem tests in the Linux dev container. Recipes and System-tier rules list path
patterns per OS. Secret storage, Recycle Bin and installed-app sources are interfaces
with per-OS backends.

### D-021: Documentation lives in docs/ — Approved (amendment A-4)
`docs/ARCHITECTURE.md`, `requirements.md` (spec plus amendments), this decision log,
`design-conflicts.md` and `phases.md`.

## Localhost security

### D-005: Single-use launch link, cookie plus header token — Approved (Phase 0)
The browser opens `http://127.0.0.1:<port>/launch?t=<token>`. The token works once and
expires after 10 minutes. It is exchanged for an HttpOnly, SameSite=Strict session cookie
and a redirect that removes it from the address bar. API calls need the cookie and the
`X-Ullage-Token` header (rendered into the page only for requests that carry the cookie).
The progress stream (EventSource cannot send headers) is the one read-only exception.
Host must equal `127.0.0.1:<port>`; Origin, when present, must match and is required on
POST. Stricter than "per-launch token" and matches the design's "already used" screen.

### D-022: Network drives are never listed or scanned — Reported (Phase 1)
Scanning a network share sends traffic over the network, which conflicts with safety
rule 1. UNC paths and remote drives are refused.

### D-023: Layered no-outbound-network enforcement — Approved (Phase 0)
An AST-based build check (`tools/nonetwork`, run by `go test ./...`) rejects shipped code
that references dialers, HTTP clients, resolvers or `exec.Command` (one reviewed
exception: `xdg-open` on Linux), restricts listeners to `127.0.0.1:0`, allowlists go.mod
modules and scans the built frontend for remote URLs. At runtime the default HTTP
transport and DNS resolver are replaced with refusing ones. The browser CSP is the last
layer.

## Scanner

### D-004: Hard links counted once via a seen-set of file IDs — Approved (Phase 0)
Windows directory enumeration returns file IDs but not link counts, and opening each file
for the count would roughly double scan time. Every file ID is tracked per volume; a file
with one link can never be seen twice, so totals equal "deduplicate when link count > 1".
About 32 bytes per file. On Linux, where `st_nlink` is free, only files with more than one
link are tracked.

### D-019: Shared hard-link bytes belong to the smallest path — Reported (Phase 1)
Parallel workers reach links in arbitrary order. Assigning the bytes to the
lexicographically smallest path makes totals identical between scans. As a result
System32 owns the bytes it shares with WinSxS, and the WinSxS recipe shows only what is
unique to the component store.

### D-010: Files kept individually from 1 MiB, small files aggregated — Reported (Phase 1)
Memory stays proportional to folders, not files. A file is kept as its own node when its
allocated or apparent size is at least 1 MiB (so large sparse images and cloud
placeholders are visible), or when a recipe claims it by path. Smaller files only add to
their folder's counters, so their names are never stored and can never appear in a
prompt.

### D-008: Unaccounted space is not a tier — Approved (Phase 0)
Volume "used" minus scanned bytes covers unreadable folders, file-system metadata,
excluded folders and data behind mount points. It is shown as an "Unaccounted" band and
ledger line, never as a fourth tier, because those bytes were never scanned.

### D-013: Dates and file-type mix recorded per node — Reported (Phase 2, amendment A-3)
Each node records modification and creation time; folders also record their three
largest file types by size. Prompts show months (`YYYY-MM`): the newest modification in
the item and its creation month. Last access time is not used: Windows updates it lazily
or not at all, so it would mislead staleness judgments.

### D-020: "Never scan these" applies to folders — Reported (Phase 1)
Exclusion patterns (`~/vault/**`, `D:/vm/**`, or a bare folder name matching at any depth)
stop the walk at matching folders. File patterns such as `*.kdbx` are not supported yet.

### D-006: Paths the Recycle Bin cannot take — Approved (Phase 0)
Some paths longer than 260 characters cannot be recycled by the shell. They are reported
as "cannot recycle"; the user may choose permanent deletion with the extra confirmation.

## Tiers and categories

### D-012: AI-first categories; recipes only pre-claim certainties — Approved (amendment A-2)
Recipes are an optimization for items whose meaning is certain and that need no AI.
Everything else goes to the AI, which invents free-text category names, so categories are
never limited to a fixed list. The prompt is written for a general audience and carries
dates and file-type mix so the AI can recognize non-developer content (video projects,
case archives, stream recordings, photo libraries). The built-in recipe set also covers
common non-developer locations (browser, chat app, media editor and game caches, phone
backups) so those need no AI round trip.

### D-017: Recipes are TOML with per-OS path patterns — Reported (Phase 1)
Variables (`{UserProfile}`, `{SystemRoot}`, `{AnyVolumeRoot}`...) expand per platform.
Validation is strict: unknown keys, bad enums, a missing reason for disallowed brute
force, and brute force allowed in a protected location (Windows, Program Files) all fail
the build's tests.

### D-018: Installed applications claim their registered folder, with a guard — Reported (Phase 1)
Each Uninstall-registry entry with an install location becomes a claim. Locations that
are too broad (a drive root, a profile, `Program Files` itself, AppData) are refused so a
sloppy installer cannot hide unrelated data. Locations match literally, never as globs.

### D-024: pagefile.sys, hiberfil.sys and WinSxS are Known, not System — Reported (Phase 1)
The design shows them under System, but the spec requires them as recipes.

## Redaction (Phase 2)

### D-025: Placeholder mapping is persisted encrypted — Reported (Phase 2)
`%APPDATA%\Ullage\rules.dpapi` holds rules and the placeholder-to-name map, encrypted
with DPAPI for the current user. Keeping the map makes `<R3>` mean the same folder across
sessions. If the file exists but cannot be read or decrypted, prompt generation is
refused (safety rule 7).

### D-026: "Whole subtree" hides every name inside, except well-known generic names — Reported (Phase 2)
Following the design, redacting a folder replaces its name and every name below it with
placeholders, so no descendant reveals anything. An option (on by default) keeps a fixed
list of generic names readable (`build`, `node_modules`, `cache`, `logs`...) so the AI
can still recognize them. "Name, keep extension" replaces only the item's own name.

### D-027: A redaction affects only what its mechanism selects — Approved (Phase 2 review)
Supersedes the first version ("a redacted name is hidden everywhere it occurs"). An item
picked in the tree (checkbox, exact-path or subtree rule) redacts that item and, with the
whole-subtree treatment, its descendants; nothing else. Glob and name-anywhere rules
redact exactly what their pattern matches. An unrelated item with a similar name stays
visible. The leak test therefore checks each redacted name within that item's own path
and its descendants' paths (the spec's "including via descendants").

### D-028: Short account names match as whole words — Approved (Phase 2 review)
Applies only to the always-on account and machine names (the old propagation is gone,
D-027). Names of 4 or more characters match as case-insensitive substrings, as the spec
asks; shorter ones match only as whole words (split at punctuation and case changes),
because a user named "Al" would otherwise redact `AppData/Local`. The owner chose this
over plain substring matching.

### D-029: Always-on redactions — Reported (Phase 2)
The current profile is rendered as `~`. Other users' profile folders, every local account
name, the machine name and Windows SIDs are redacted wherever they appear. Names that
contain `<` or `>` (possible on Linux) have those characters replaced so they cannot
imitate a placeholder.

### D-030: Redacted items carry no metadata except size — Approved (amendment A-3, confirmed in Phase 2 review)
An item whose own name is redacted is sent with its ID, rounded size and placeholder only:
no dates, no file count, no file types. Items that are merely inside a redacted folder
roll up into that folder's line. Implemented in the Phase 3 prompt builder, which reads
`NodeState.NameHidden()`.

### D-031: Extensions are never sensitive — Approved (Phase 2 review)
Supersedes the first version, which dropped an extension that contained a sensitive word.
"Name, keep extension" always keeps a normal extension (up to 10 letters or digits). An
extension that is not normal (spaces, symbols, longer) is treated as part of the name.

### D-032: Small files are never listed individually — Approved (Phase 2 review)
Files under 1 MiB are aggregated (D-010), so the redaction review shows them as "N small
files (names never sent)" instead of one row per file. The design shows an 88 KB file as
its own row; such a file's name cannot leave the machine at all, so there is nothing to
redact. Approved: only a folder whose collection of small files is large enough to be
worth examining reaches the prompt, as one line (Phase 3 rollup).

### D-033: Unreadable rules are moved aside only on explicit confirmation — Reported (Phase 2)
While the rules file is unreadable, every redaction operation fails and nothing writes to
the file. The user can choose "Move it aside and start over" (a two-step button; the API
requires the literal confirmation `reset`), which renames the file to
`rules.dpapi.unreadable-<timestamp>` so it can still be recovered, for example by copying
it back to the Windows account that created it.

### D-034: Placeholder numbers are global and persisted — Reported (Phase 2)
One registry maps names to numbers for all scans, saved in the encrypted rules file, so
`<R3>` stays the same folder in later sessions and the AI can correlate items across
batches. Names hidden inside a redacted subtree get numbers too; the registry never
leaves the PC.

## Presentation

### D-015: Sizes in 1024-based units labelled GB — Reported (Phase 1)
Matches Windows Explorer, so numbers agree with what users see elsewhere on the PC.
Strictly these are GiB; switching to decimal units is a one-line change.

### D-011: Design "Unreadable" and "Last-modified month" — see design-conflicts.md

## Prompts and replies (Phase 3)

### D-035: How the Unknown tier becomes lines — Reported (Phase 3); batch size changed by D-069
1. Forced splits: a folder that also holds Known or System bytes is split until every
   item is purely Unknown, so an ID can never cover recipe or System data.
2. Budgeted refinement: the largest remaining folder is replaced by its children of at
   least the minimum size (default 100 MiB) plus a `folder/*` line for the rest, while
   the item budget (default 900, about three batches) allows.
3. A folder stays whole when its name already says what it holds (`node_modules`, `.git`,
   `build`, `cache`, `Render Files`...), or when one file type makes up 80% of it and its
   children are either more than 20 files or folders with generic names (years, months).
Large things get detail; small or uniform ones stay one line. Thresholds are in
`config.json` (`promptMinimumMegabytes`, `promptBatchLines`, `promptMaximumItems`).

### D-036: Line notation — Reported (Phase 3)
`ID SIZE PATH` then, when the name is shown: `xN` (same-named folders collapsed, one ID
each as a range), `[N files]` (when more than one), `mYYYY-MM` (newest change inside),
`cYYYY-MM` (created, omitted when equal to m), `{ext,ext}` (types with at least 10% of
the bytes). `folder/*` is the rest of a folder not listed separately. Header lines hold
shared prefixes, chosen by character cost. Redacted items show only ID, size and
placeholder (D-030). Numeric and date-like names are never collapsed.

### D-037: IDs are global; a reply is validated against the batch the user chose — Reported (Phase 3)
IDs run continuously across batches, so a reply's IDs must lie inside the selected
batch's range. The reply format stays exactly as specified (no batch field).

### D-038: A rebuilt export gets a new session code — Reported (Phase 3)
The first export uses the scan's code. If redactions change before any reply is
imported, the export is rebuilt with a new code, so a reply to the superseded prompt is
rejected as a session mismatch instead of being mapped onto different items. After an
import, a redaction change marks the export stale; the import is kept until the user
rebuilds explicitly.

### D-039: What the parser tolerates — Approved (Phase 3 review: no synonyms)
Tolerated: prose around the block, fences, bold markers, Markdown tables, bullets,
blockquotes, smart quotes, typographic dashes in ranges, zero-width and non-breaking
characters, CRLF, upper-case enums and headers, and the prompt pasted before the reply
(the last `@END` and the `@SESSION` before it are used). Not tolerated: synonyms such as
`medium` (the spec's enums are closed), names over 80 characters, rows without IDs. An
empty block is valid and means nothing was recognized. Any problem blocks the import.

### D-040: Names lose invisible format characters — Reported (Phase 3)
Found by the injection test: a right-to-left override (U+202E) is a format character,
not a control character, and can make `exe.txt` display as `txt.exe`. All Unicode
format characters are replaced like control characters.

### D-041: Deleting a `folder/*` item — Resolved by D-053, approved (Phase 5)
Small files have no stored names (D-010). Deleting a loose-contents item will mean
re-listing the folder at deletion time and removing what was not listed separately,
under the same re-verification as every other item. To be designed in Phase 5.

### D-042: Token estimates are offline approximations — Reported (Phase 3)
The UI shows characters / 4. `tools/tokenestimate` (dev only) counts letter runs, digit
runs and symbols; expect ±30%. A typical mixed PC (320 GB Unknown) produced 108 items in
39 lines, about 1,300 tokens including instructions.

## Results and round B (Phase 4)

### D-043: What the Results list contains — Reported (Phase 4)
(Partly superseded by D-078: `del 0` rows can be selected after "Delete anyway".)
- Recipe categories: one per recipe, summing the Known bytes of every place its claims
  start. Bytes of a nested claim (a recipe path inside another recipe's) count only for
  the nested one. Installed applications are left out (their recipe says keep), and so
  is any category under the prompt's minimum item size (default 100 MiB).
- AI categories: one per imported row, with the export items its IDs name. Rows marked
  `del 0` are not reclaimable: they are listed under "the AI suggests keeping", shown on
  request, and cannot be selected.
- Both are sorted by bytes, largest first. A summary accounts for System, Known that
  recipes keep, Unknown kept by the AI and Unknown not classified, so an empty result
  still explains where the space is.

### D-044: Numbering and rounds for round B — Superseded by D-066
Every imported category gets a number unique within the export, assigned at import.
Round B refers to categories only by these numbers. Importing a batch again gives its
categories new numbers and drops their explanations, queue entries and unlocks. The
round B prompt uses the export's session code and is the step after the batches (round
4 after three batches, round 5 after the next explanation round), as in the design.

### D-045: What the round B parser tolerates — Superseded by D-066
Same extraction as round A (prose, fences, quotes, last block). Accepted: bullets, bold
markers, `### #2` headings, `Step 1:` and upper-case keys. Required per category:
exactly one `what:`, one `lost:` and 1 to 5 `step:` lines. The prompt asks for under 25
words per line; above 40 words or 320 characters the reply goes back for repair.
Numbers that were not asked about, and duplicates, are rejected. Requested categories
the reply leaves out are not an error: they stay queued. Control and format characters
are removed from the text, which is only ever displayed.

### D-046: AI cleanup steps are text, not commands — Reported (Phase 4)
The design shows AI categories with copyable `$ command` lines. A command written by the
assistant is an action it authored (safety rule 3), and a copy button would present it
with the program's endorsement. AI steps are shown as plain advice, labeled "advice from
the AI"; only recipe steps, which ship with Ullage, have command lines with a copy
button. See design conflict 15.

### D-047: How real names are restored — Reported (Phase 4)
Placeholders in category names and explanations are replaced locally using only the
placeholders of items in the current export and their folders, rendered by the same
redactor the prompt was built with. A placeholder the export never used stays as it is.
Restored text is only displayed; nothing restored is ever sent back to the assistant.

### D-048: The low-confidence unlock is recorded by the server — Reported (Phase 4)
Opening a low-confidence row posts `/api/results/expand`; the server records it and only
then reports the category as selectable. Phase 5 can therefore refuse a deletion of a
low-confidence category nobody opened, not only hide the checkbox.

### D-049: Selection lives in the browser until Phase 5 — Reported (Phase 4)
Checkboxes select items (or a whole category, including items beyond the 200 listed).
The selection is kept in the page and pruned when results change; "Review deletion" is
disabled until step 7 exists. Phase 5 will send the selected keys and re-resolve every
one on the server.

### D-050: AI categories need an explanation before delete — Approved (Phase 4), revised by D-066
The design says "Cleanup methods arrive with the explanation. Until then these items
can't be selected." The spec only gates low confidence; on review the stricter design
rule was adopted. An AI category can be selected only after its round B explanation has
been imported, so the user has read what is lost; a low-confidence one must also have
been opened. Recipe categories carry their explanation and are not affected. The server
reports both locks (`needsExplanation`, `needsExpand`), so Phase 5 can enforce them.

## Deletion (Phase 5)

### D-051: What "unchanged since the scan" means — Approved (Phase 5 review)
A re-walk of the item (links counted, never followed) must match the scan exactly in
file count, folder count, link count, total logical size and newest modification time
(of any file, folder or link inside); for a single file, allocated size too. Logical
rather than allocated size is compared for folders because a hard-linked file's bytes
are counted at whichever link the scan reached first, so a whole-drive scan and a re-walk
of one folder can disagree without anything having changed. Any difference blocks the
item, and the reason says what changed. Every folder from the scanned root down to the
item must also still be the same folder (by file ID) and not a link, so a folder swapped
for a junction after the scan cannot redirect the deletion.

### D-052: What is never deleted, beyond the spec's list — Revised on review (Phase 5)
In addition to the System tier, the Windows folder and Program Files (safety rule 4):
scanned roots as a whole; any user profile, the profiles folder, Ullage's configuration
folder and every folder containing one; items containing anything of another tier or
category (a recipe folder holding another recipe's folder, an AI folder holding a recipe
folder); and folders that were excluded or could not be read, which cannot be verified.

Cloud-synced items (inside or holding a folder managed through a non-link reparse point,
such as OneDrive) and cloud-only placeholders were refused at first. On review they are
allowed, with a warning: a "cloud-synced" badge on the category, a note in its details
saying how many items are affected and how much is only in the cloud (and frees nothing
here), a line on each such item in the confirmation, and a summary note above the
Recycle Bin and permanent options. The Windows directory reader flags such entries.

### D-053: Deleting loose contents (`folder/*`) — Approved (Phase 5 review)
A loose-contents item deletes the small files directly in the folder and the unlisted
subfolders that belong entirely to its category. Listed items, links and subfolders
mixing categories stay. Small files have no stored names, so they are verified in
aggregate (count, logical size, newest modification); any new entry in the folder,
including a new folder or link, blocks the item.

### D-054: How the Recycle Bin is used — Approved (Phase 5 review)
Windows: `SHFileOperationW` with undo, no confirmation, no error UI and
`FOF_WANTNUKEWARNING`, so when Windows cannot recycle something (too large for the bin,
a drive without one) the shell asks before deleting permanently, and declining is
reported as a failure; Ullage never turns a recycle into a silent permanent deletion.
Paths of 260 characters or more cannot go through the shell and are reported, so the
user can choose permanent deletion. Linux (for the future port): the freedesktop.org
home Trash, only for items on the same file system. Free space returns when the bin is
emptied, which the final screen says.

### D-055: Permanent deletion — Approved (Phase 5 review)
Behind the typed confirmation `delete N` (N ready items), checked on the server, not
only in the page. Folders are deleted by Ullage's own walk using the same directory
reader as the scan, so junctions and symbolic links inside are removed as links and
their targets are never touched (tested with real junctions on Windows CI and symbolic
links on Linux). A failure on one file does not stop the rest; the item is then
reported as partly removed.

### D-056: Plans and runs — Approved (Phase 5 review)
Opening the confirmation creates a plan: every item verified, with an ID valid for 15
minutes and usable once. Executing re-resolves the selection (a category that became
unselectable is not deleted) and verifies each item again just before deleting it. One
run at a time, in the background; failures do not stop it; "Stop after current item"
skips the rest. A new scan cannot start during a run.

### D-057: After deleting — Approved (Phase 5 review)
Deleted items, recorded by scan node, leave the Results list (a category emptied by
deleting disappears). The Overview keeps the scan's numbers until the next scan; Phase 6
adds history of what was reclaimed.

### D-058: What the browser may send — Approved (Phase 5 review)
Selection keys name a category and an item of the current Results (`ai:3|id:42`,
`recipe:npm-cache|node:17`, or `ai:3/rest` for items beyond the 200 listed). The server
resolves them itself; a key that names nothing deletable is listed as refused with the
reason, never silently dropped, and never interpreted as a path.

## History, launch and packaging (Phase 6)

### D-059: What history keeps — Reported (Phase 6)
One entry per scan: start and end time, drive names (`C:`; a folder scan records only
its drive), used space before and after deleting, batches imported, items deleted,
bytes sent to the Recycle Bin, bytes deleted permanently, and failures. No file or folder
names. The file is encrypted for the current user like the rules (DPAPI on Windows) and
lives in `%LOCALAPPDATA%\Ullage\history.dpapi`. An unreadable file is moved aside and
history starts over: unlike the rules, history decides nothing, so it need not fail
closed. "Clear history" asks twice. The design's "Still restorable in the Recycle Bin"
became "Sent to the Recycle Bin": Ullage cannot tell whether the bin was emptied since.

### D-060: Single instance through a named pipe — Reported (Phase 6)
The first copy creates `\\.\pipe\Ullage-<user SID>` with `FILE_FLAG_FIRST_PIPE_INSTANCE`,
which is the lock; the pipe's ACL allows only the current user and remote clients are
rejected. A second launch writes `launch` and receives a fresh one-time launch link,
checks that it points to `http://127.0.0.1:`, opens it and exits. No TCP is involved, so
the no-network check needed no exception. Linux has no single-instance handling yet.

### D-061: Start menu shortcut — Reported (Phase 6)
Created on the first run (not when running from the temp folder, as `go run` does), and
managed with `--install-shortcut` and `--remove-shortcut`. The `.lnk` is written directly
in the documented format, with the item ID list obtained from the shell
(`SHParseDisplayName`), so there are no COM objects and no cgo. Windows CI reads it back
through `WScript.Shell`, including a non-ASCII folder name. Moving the executable breaks
the shortcut; running `--install-shortcut` from the new place fixes it.

### D-062: Release build — Reported (Phase 6)
One executable with the UI embedded, built with `-H windowsgui` so no console window
opens, version stamped with `-X main.version`. Without a console, fatal errors appear in a
message box. `scripts/build.sh` and `scripts/build.ps1` produce `bin\ullage.exe`.

### D-063: Log files, redacted — Revised on review (Phase 6)
At first no log file was written. On review, logs are written with sensitive information
redacted: `%LOCALAPPDATA%\Ullage\logs\ullage-YYYYMMDD.log`, one file per day, kept 14
days, at most 10 MB per day. Every line is built from safe parts:

- Paths go through the same redactor as prompts (`~`, `<Rn>` placeholders, account and
  machine names hidden). Below the deepest scanned node, names are shown as `…`, because
  the scan never recorded them and they cannot be checked against the rules.
- Paths outside the scan, and every path while the rules cannot be read, are written as
  `<path withheld>` (fail closed, like safety rule 7).
- Scan events are held in memory and written when the scan finishes, since paths can be
  rendered only once the tree exists; a failed scan logs its error with paths withheld.
- Free text such as error messages is scrubbed: embedded paths rendered as above, account
  names and the machine name replaced by `<account>`, SIDs by `<sid>`.
- Never logged: rule patterns (they may name what is hidden), prompt text, reply text.

A leak test scans names covered by a rule, an always-redacted word, a SID and a link to
a folder outside the scan, runs export and paste, and asserts that none of them reaches
the log; a second test asserts that all paths are withheld while the rules are
unreadable. Both fail when rendering is bypassed.

### D-064: End-to-end test level — Reported (Phase 6)
The required "end-to-end on Windows CI" test drives a real server over loopback the way
the browser does (launch link, cookie, token header, Origin): scan a temporary folder,
export, import a reply with its explanations, verify, recycle with the real Recycle Bin, and
read the history. Browser-level runs (headless Chromium) are done during development;
they are not part of CI, to keep it free of browser downloads.

## Releases

### D-065: Release artefacts — Reported
A `v*` tag runs `.github/workflows/release.yml` on Windows: the full test suite and the
no-network check, then `ullage-<version>-windows-amd64.exe` and `-arm64.exe` (cgo off,
GUI subsystem, version stamped), a smoke test that the amd64 build starts and reports
its version, a zip per architecture with the README and `THIRD-PARTY-NOTICES.txt`, and
`SHA256SUMS.txt`. Build provenance is attested with GitHub's attestation action, so a
download can be checked with `gh attestation verify`; GitHub does not offer this for
private personal repositories, so while the repository is private the step is skipped. The notices file is generated
from the build graph (`scripts/notices.sh`): the Go standard library, every linked Go
module, the npm runtime dependencies bundled into the UI, and the IBM Plex fonts (OFL).
The executables are not Authenticode-signed (no certificate), so SmartScreen may warn
on first run. A manual run rehearses everything without publishing.

### D-076: Automatic releases from main — Approved (owner request)
Every push to `main` that changes the program publishes a release; pushes that touch
only `docs/`, `AGENTS.md` or the CI workflow do not. A first job on Linux picks the
version: the highest stable `vX.Y.Z` tag with its patch number bumped, or its minor or
major number when a commit since that tag has `[minor]` or `[major]` in its subject
line (bodies are ignored, so they can mention the markers). The head commit with
`[skip release]` in its subject, or already carrying a `v*` tag, skips the release. The Windows
job then runs exactly as for a pushed tag (D-065) and `gh release create --target`
creates the tag on the commit it built; a tag made with the workflow's token starts no
further run. Pushing a tag by hand still publishes that exact version, which is how
pre-releases are made. Runs share one concurrency group, so two quick pushes cannot pick
the same number. Only Windows executables are published: the product is Windows-only,
the Linux build exists to keep the platform seam honest, and there is no macOS port.

## Explanations in the reply, one Delete step

### D-066: Explanations are part of every reply — Approved (owner request)
Explanations are no longer requested per category in a separate round B. The batch
prompt asks for them directly: below each category row, a `what:`, a `lost:` and 1 to 5
`step:` lines. They are required for a row with `del 1` and optional for `del 0`. The
parser finds them by their prefix before treating a line as a row, so their text may
contain `|`. A deletable row without them, a second `what:` or `lost:`, more than five
steps, an explanation line before the first row, or a line over 40 words or 320
characters is a problem under the new "Explanations" check, and the reply goes back
with a repair prompt like any other problem. The word target (25) and step limit (5)
are stated in the prompt; a test keeps them equal to what the parser enforces. D-050
still holds: an AI category without an explanation cannot be selected; since the parser
rejects such rows, that lock is now a second line of defense. Round B, its queue, its
prompt, its parser, `/api/results/explain` and the request-explanation buttons are
removed.

### D-067: Results and Delete are one step — Approved (owner request)
The flow is six steps: Scan, Overview, Redaction, Export prompt, Paste response, Delete.
Step 6 opens on the category list (formerly step 6, Results): each row shows the
explanation's "what" and "lost if deleted" directly under it, and the detail still
shows the cleanup steps and items. "Review deletion" switches the same page to the
verification, confirmation and progress view (formerly step 7); "Back to the list" and
finishing return to the list. Number key 6 opens the step; 7 does nothing.

### D-068: Brute force with a warning — Approved (owner request)
Some recipes were managed-only because the owning tool is the better way to clean up,
not because deleting is unsafe. That left nothing to do when the tool was already gone
(an uninstalled rustup leaves its toolchains behind). Such recipes now allow brute force
and carry a `brute_force.warning` that says what can break. The warning is shown as a
badge on the row, in the brute-force box, on each confirmation row and once per
category above the delete options. Recipe validation rejects a warning on a recipe that
does not allow brute force. Changed (version bumped): Rust toolchains, Docker Desktop
disk image, WSL distro disks, Ollama models, Visual Studio package cache, iPhone and
iPad backups, Explorer thumbnail cache. Still managed-only: everything under the Windows
folder (delivery optimization, system temp, Windows Update downloads, component store,
previous Windows), the hibernation and page files, restore points, the Recycle Bin and
installed applications. The deletion guard is unchanged: protected trees and System
tier stay blocked whatever a recipe says.

### D-069: Smaller batches — Reported
Replies now carry about four lines per deletable category instead of one, so the
default data-line budget per batch drops from 300 to 150. Long replies are where chat
assistants truncate output, and a truncated reply costs a repair round; more batches
cost one more copy and paste each. The item budget (900) and minimum size (100 MiB) are
unchanged; `promptBatchLines` in `config.json` still overrides the default.

## Deleting what changes, what is old, and seeing the prompt

### D-070: Folders that change all the time are split into entries — Approved (owner request)
A recipe can set `split_entries = true`. Each entry directly inside its folder is then
its own item, plus one loose item for the folder's small files (which have no stored
names, D-010). Every item is verified on its own, so one new file in `%TEMP%` blocks
only the entry it landed in, not 25 GB of old temporary files. Only the user temp recipe
uses it for now. Loose items keep their existing rules: anything added since the scan
counts as a change.

### D-071: An age filter for deleting what has not changed in a long time — Approved (owner request)
The Delete step has "Unchanged for: any time / 1 month / 6 months / 1 year / 2 years /
5 years". With a filter, only items whose newest change inside is older are listed and
can be selected; the rest of each deletable category is reported as "changed in the
last …" in the footer, and a category with nothing left is not listed. The filter is
applied on the server, before items are trimmed for display, and deletion resolves the
selection against the same filtered list, so "select all" never reaches a newer item.
An item whose change time is unknown counts as recent. Categories the AI suggests
keeping are not filtered.

### D-072: Re-scanning changed items — Approved (owner request)
Items blocked as "changed since the scan" can be re-scanned from step 6. Re-scanning
re-runs every check; only an item whose contents differ, and that passes every other
check (location, tier, ancestors), is read again. Its current state becomes the new
baseline, shown with its new size for the user to confirm. Any change after the re-scan
blocks it again (safety rule 6 is measured from the latest look, never skipped).
Baselines last for the scan and are dropped by a new scan.

### D-073: Categories to keep are always listed — Approved (owner request)
(Partly superseded by D-078: they can be selected after "Delete anyway".)
The Delete list shows the AI's "keep" categories under their own heading after the
deletable ones, instead of behind a link in the footer. They still cannot be selected.

### D-074: The redaction review shows the real prompt lines — Approved (owner request)
The inspector's "In the prompt" section shows the lines an export would contain for the
selected item and everything inside it, after redaction and exactly as copied except for
the item numbers: up to 8, largest first, with the total. An item without a line of its
own shows the larger line that includes it. Hiding a folder as a whole tree makes it one
line with only a size, which the assistant can only suggest keeping; the inspector says
so. This is what happened to a 135 GB home folder in practice. The rules manager's
"+ N more" loads every match of a rule (up to 1,000).


### D-075: A hidden home folder keeps reading ~ and keeps its profile layout — Reported
A whole-tree rule on `~` turned the home folder into `<Rn>` and, because no name inside
it was shown, into one line with only its size (D-074 saw this happen to 135 GB).
"Keep well-known folder names" did not help: none of the profile's own folders were on
the list, so the selector had nothing it could split on. Now:
1. The current profile folder keeps reading `~` under a redaction, since `~` says
   nothing about the user. It is still treated as redacted: no dates, counts or types on
   its own lines (D-030), and every name inside stays hidden.
2. The well-known list includes the standard profile layout on Windows, macOS and Linux
   (`AppData`, `Local`, `Roaming`, `Documents`, `Downloads`, `Pictures`, `Videos`,
   `Library`, `.config`, `.local`, `.cargo`, `.npm`...). These are the same on every PC.
   With the option on (the default), a hidden home splits along them, for example
   `~/AppData/Local/Temp`, `~/Videos` and a size-only `~/*` for the rest.
3. The inspector's one-line warning now says what to do next: turn the option on, or,
   when it is already on, use "Redact name, keep extension", which lists what is inside
   by size under placeholders.
Hidden names still never roll out of their folder's line one by one, so "whole subtree"
keeps meaning that the structure of non-generic folders is not sent.

### D-077: "Select all" buttons on the Delete list — Approved (owner request)
Above the category list, "Select all" selects every listed deletable category, and when
both recipe and AI categories are listed, "Select all recipes" and "Select all AI" select
one source. Each button shows how many categories it would select and turns into
"Deselect" once they all are. They act on the rows the filters list (source, confidence,
regenerable, age), so what is selected is what is on screen. Categories that cannot be
selected are skipped and counted in a note: the ones the AI suggests keeping (D-043,
D-073), managed cleanup only, unexplained, and low-confidence rows not yet opened, so a
bulk selection never unlocks what a single checkbox would not. Key `a` toggles "Select
all". The confirmation step and its re-verification (step 6's second half) are unchanged.

### D-078: "Delete anyway" for categories the AI suggests keeping — Approved (owner request)
Supersedes the "cannot be selected" part of D-043 and D-073. A category the AI marked
`del 0` stays under "The AI suggests keeping", unselectable, until the user opens its row,
chooses "Delete anyway…", and types `delete anyway` exactly. The server checks the phrase
(`POST /api/results/override-keep`), records the override for that category, and from
then on the row can be selected like a deletable one: its checkboxes, its own "Select
all", and the D-077 "Select all" buttons, which still skip every keep category without the
override. "Keep again" takes the override back and drops its items from the selection;
a plan prepared before that deletes nothing of it, because execution resolves the
selection again. An override lasts for the export: importing the batch again gives the
category a new number, and the override lapses with it.
Only the "keep" guard is relaxed. A keep row needs no explanation (the AI is not asked
for one), so the typed phrase stands in for D-050's. Everything else applies unchanged:
low-confidence rows must be opened, the age filter (D-071) now applies to the category,
managed-cleanup-only recipes cannot be overridden, and every safety check, the Recycle
Bin default and the typed confirmation for permanent deletion still run at step 6. The
row, the confirmation list and the deletion progress mark these items "deleting anyway"
so the choice stays visible until the end.
