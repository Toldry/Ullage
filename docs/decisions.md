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

### D-035: How the Unknown tier becomes lines — Reported (Phase 3)
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

### D-041: Deleting a `folder/*` item — Resolved by D-053 (Phase 5)
Small files have no stored names (D-010). Deleting a loose-contents item will mean
re-listing the folder at deletion time and removing what was not listed separately,
under the same re-verification as every other item. To be designed in Phase 5.

### D-042: Token estimates are offline approximations — Reported (Phase 3)
The UI shows characters / 4. `tools/tokenestimate` (dev only) counts letter runs, digit
runs and symbols; expect ±30%. A typical mixed PC (320 GB Unknown) produced 108 items in
39 lines, about 1,300 tokens including instructions.

## Results and round B (Phase 4)

### D-043: What the Results list contains — Reported (Phase 4)
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

### D-044: Numbering and rounds for round B — Reported (Phase 4)
Every imported category gets a number unique within the export, assigned at import.
Round B refers to categories only by these numbers. Importing a batch again gives its
categories new numbers and drops their explanations, queue entries and unlocks. The
round B prompt uses the export's session code and is the step after the batches (round
4 after three batches, round 5 after the next explanation round), as in the design.

### D-045: What the round B parser tolerates — Reported (Phase 4)
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
The round B prompt always uses the assistant's original text, never the restored one;
a test asserts that no redacted name reaches it.

### D-048: The low-confidence unlock is recorded by the server — Reported (Phase 4)
Opening a low-confidence row posts `/api/results/expand`; the server records it and only
then reports the category as selectable. Phase 5 can therefore refuse a deletion of a
low-confidence category nobody opened, not only hide the checkbox.

### D-049: Selection lives in the browser until Phase 5 — Reported (Phase 4)
Checkboxes select items (or a whole category, including items beyond the 200 listed).
The selection is kept in the page and pruned when results change; "Review deletion" is
disabled until step 7 exists. Phase 5 will send the selected keys and re-resolve every
one on the server.

### D-050: AI categories need an explanation before delete — Approved (Phase 4 review)
The design says "Cleanup methods arrive with the explanation. Until then these items
can't be selected." The spec only gates low confidence; on review the stricter design
rule was adopted. An AI category can be selected only after its round B explanation has
been imported, so the user has read what is lost; a low-confidence one must also have
been opened. Recipe categories carry their explanation and are not affected. The server
reports both locks (`needsExplanation`, `needsExpand`), so Phase 5 can enforce them.

## Deletion (Phase 5)

### D-051: What "unchanged since the scan" means — Reported (Phase 5)
A re-walk of the item (links counted, never followed) must match the scan exactly in
file count, folder count, link count, total logical size and newest modification time
(of any file, folder or link inside); for a single file, allocated size too. Logical
rather than allocated size is compared for folders because a hard-linked file's bytes
are counted at whichever link the scan reached first, so a whole-drive scan and a re-walk
of one folder can disagree without anything having changed. Any difference blocks the
item, and the reason says what changed. Every folder from the scanned root down to the
item must also still be the same folder (by file ID) and not a link, so a folder swapped
for a junction after the scan cannot redirect the deletion.

### D-052: What is never deleted, beyond the spec's list — Reported (Phase 5)
In addition to the System tier, the Windows folder and Program Files (safety rule 4):
scanned roots as a whole; any user profile, the profiles folder, Ullage's configuration
folder and every folder containing one; items containing anything of another tier or
category (a recipe folder holding another recipe's folder, an AI folder holding a recipe
folder); folders that were excluded or could not be read, which cannot be verified;
cloud placeholders, whose deletion frees nothing here and deletes the cloud copy; and
anything managed through a non-link reparse point, such as cloud-sync folders. That
last one is new: the Windows directory reader now flags such entries.

### D-053: Deleting loose contents (`folder/*`) — Reported (Phase 5)
A loose-contents item deletes the small files directly in the folder and the unlisted
subfolders that belong entirely to its category. Listed items, links and subfolders
mixing categories stay. Small files have no stored names, so they are verified in
aggregate (count, logical size, newest modification); any new entry in the folder,
including a new folder or link, blocks the item.

### D-054: How the Recycle Bin is used — Reported (Phase 5)
Windows: `SHFileOperationW` with undo, no confirmation, no error UI and
`FOF_WANTNUKEWARNING`, so when Windows cannot recycle something (too large for the bin,
a drive without one) the shell asks before deleting permanently, and declining is
reported as a failure; Ullage never turns a recycle into a silent permanent deletion.
Paths of 260 characters or more cannot go through the shell and are reported, so the
user can choose permanent deletion. Linux (for the future port): the freedesktop.org
home Trash, only for items on the same file system. Free space returns when the bin is
emptied, which the final screen says.

### D-055: Permanent deletion — Reported (Phase 5)
Behind the typed confirmation `delete N` (N ready items), checked on the server, not
only in the page. Folders are deleted by Ullage's own walk using the same directory
reader as the scan, so junctions and symbolic links inside are removed as links and
their targets are never touched (tested with real junctions on Windows CI and symbolic
links on Linux). A failure on one file does not stop the rest; the item is then
reported as partly removed.

### D-056: Plans and runs — Reported (Phase 5)
Opening the confirmation creates a plan: every item verified, with an ID valid for 15
minutes and usable once. Executing re-resolves the selection (a category that became
unselectable is not deleted) and verifies each item again just before deleting it. One
run at a time, in the background; failures do not stop it; "Stop after current item"
skips the rest. A new scan cannot start during a run.

### D-057: After deleting — Reported (Phase 5)
Deleted items, recorded by scan node, leave the Results list (a category emptied by
deleting disappears). The Overview keeps the scan's numbers until the next scan; Phase 6
adds history of what was reclaimed.

### D-058: What the browser may send — Reported (Phase 5)
Selection keys name a category and an item of the current Results (`ai:3|id:42`,
`recipe:npm-cache|node:17`, or `ai:3/rest` for items beyond the 200 listed). The server
resolves them itself; a key that names nothing deletable is listed as refused with the
reason, never silently dropped, and never interpreted as a path.
