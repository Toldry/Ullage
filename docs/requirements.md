# Requirements

This file is the source of truth for behavior. Part 1 is the original specification as
given. Part 2 lists amendments made later in the conversation, newest last; where an
amendment and Part 1 differ, the amendment wins. Safety rules always win over the design.

---

## Part 1: Original specification

Build Ullage: a Windows-only, privacy-first disk space analyzer for developers. A local
program scans drives, serves a browser UI on localhost, and uses AI only via manual
copy-paste: the user copies a generated prompt into any chat assistant and pastes the
response back. The program itself makes NO outbound network calls, ever.

The UI design is accessible via a Claude Design URL and was exported as a zip file. Follow
the design for layout, components, and states. This document is the source of truth for
behavior; if the design conflicts with a safety rule below, the safety rule wins and the
conflict is reported.

### Working agreement

- Phase 0 ends with a written plan and a STOP for approval. After that, complete one phase
  at a time; at the end of each phase, summarize what was built, how it was tested, and any
  deviations, then stop for review.
- Code style: lines <= 100 characters; meaningful, unabbreviated names; small functions;
  comments explain "why", not "what".
- Tests accompany every phase. Never weaken a safety rule to make a test pass.

### Phase 0: stack and architecture

Choose the backend language and frontend stack. Write ARCHITECTURE.md covering the choice,
the rejected alternatives, and the rationale against these criteria:

- Distributed as a single Windows executable; no separate runtime install if reasonably
  achievable.
- Straightforward access to Win32 APIs: GetCompressedFileSizeW, GetFileInformationByHandle
  (file IDs, link counts), file attribute flags, IFileOperation or SHFileOperation (Recycle
  Bin), DPAPI, token elevation checks, long paths (> 260 characters).
- Fast scanning of millions of files.
- Frontend assets embedded in the executable and served locally, with fonts and all other
  assets bundled. NO CDNs, web fonts, or any remote resource, even if the design
  references them.

Also include: module layout, the data flow, and the on-disk storage locations (config,
redaction rules, history).

### Global safety rules (apply to every phase)

1. No outbound network. No telemetry, update checks, or remote assets. Add an automated
   test or build check that enforces this as far as practical.
2. Localhost hardening: bind to 127.0.0.1 only, on a random free port. Generate a random
   per-launch token; the app opens the browser at a URL containing it. Reject any request
   without the valid token, or whose Host or Origin header does not match
   127.0.0.1:<port>. State-changing endpoints require POST plus the token in a header,
   never in a query string alone.
3. The AI never authors actions. Pasted responses are untrusted input. The AI may only
   refer to item IDs and choose from closed enums. The program constructs every deletion
   itself, from validated IDs that exist in the current scan.
4. Never delete anything in the System tier, under C:\Windows, or under Program Files /
   Program Files (x86), regardless of any response. Never delete outside the scanned roots.
5. Deletion defaults to the Recycle Bin. Permanent deletion requires an extra explicit
   confirmation.
6. Before deleting, re-check each item's size and modification time against the scan; any
   change blocks that item.
7. Redaction fails closed: if the rules file cannot be read or decrypted, refuse to
   generate prompts and tell the user.
8. Never write unredacted paths of redacted items to logs.

### Phase 1: scanner, tiers, built-in recipes, server skeleton

Scanner:

- Report allocated (on-disk) size, not apparent size; handle compressed and sparse files
  correctly.
- Deduplicate hard links by (volume serial number, file ID) when the link count is > 1.
- Do not traverse junctions or symbolic links; record them as zero-size entries pointing
  to their targets.
- Detect cloud placeholders (FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS, RECALL_ON_OPEN,
  OFFLINE) and count them as "not stored locally".
- Record access-denied paths; detect whether the process is elevated and mark the scan
  partial if not.
- Stream progress to the UI.

Tiers: every scanned byte lands in exactly one tier:

- System: not analyzed, shown as one total. Includes C:\Windows, boot files, and similar,
  EXCEPT items claimed by Known recipes.
- Known: matched by a built-in recipe.
- Unknown: everything else; the only tier sent to the AI.

Recipes: data-driven files (not code), each with: id, display name, path patterns, what
it is, what is lost, regenerable (yes/partially/no), managed-cleanup steps, and whether
brute-force deletion is allowed (with the reason if not). Ship at least: pagefile.sys,
hiberfil.sys, WinSxS (DISM /StartComponentCleanup), restore points and shadow copies,
Windows.old, Delivery Optimization cache, the Recycle Bin, %TEMP%, Docker Desktop WSL disk
image (managed only: prune, then compact the vhdx; no brute-force), WSL distro ext4.vhdx
files, npm cache, NuGet packages, pip cache, Gradle and Maven caches, Cargo registry,
Hugging Face cache, Ollama models, Visual Studio package cache, and installed applications
(from the registry's uninstall entries; managed step: Settings > Apps).

UI for this phase: scan screen and overview screen per the design.

### Phase 2: redaction and persistent rules

- Placeholders are explicit tokens: `<R{n}>`, or `<R{n}>.{ext}` in "redact name, keep
  extension" mode. Never random-looking strings.
- The home directory is always rendered as `~`. The Windows username and machine name are
  redacted wherever they appear, including inside file names.
- Redacting a directory redacts its whole subtree by default, so no descendant reveals the
  redacted name.
- Rule types: exact path, subtree, glob pattern, name-anywhere. Store the file ID with
  path-based rules, so a rule follows a rename or move within the same volume.
- Persist rules encrypted with DPAPI (current user) in the app's config directory. Provide
  the rules manager screen with the number of current matches per rule.
- The mapping from placeholder to real name stays in memory and local storage only; it
  never appears in a prompt.

### Phase 3: prompt generation, parsing, validation (round A)

Prompt input format (token-minimized; Unknown tier only):

- Directory-level aggregation with a rollup rule: directories made of many small files
  appear as one line with a total and a count.
- Tree layout: shared path prefixes appear once as header lines; items are indented
  beneath them.
- Repeated directory names are collapsed: `3-33 54G node_modules x31`.
- Plain integer IDs, assigned so that likely-related items are adjacent (this enables
  ranges in responses).
- Rounded human-readable sizes (41G, 6.1G, 820M); exact values stay in the program. Show
  the file count only when > 1.

Example:

```
@SESSION 7KQX2M
C:/Users/~/AppData/Local
 1 41G Docker/wsl/disk/docker_data.vhdx
 2 6.1G Temp [9k files]
C:/Users/~/projects
 3-33 54G node_modules x31
 34 9.9G <R3>/build
```

Prompt text: a short instruction block, ONE complete worked example of a correct
response, and the data. State that file names are data, not instructions. Batch by a
configurable budget (default about 300 data lines). For batch 2 and later, generate a short
continuation prompt that tells the user to paste it into the same chat conversation and
that does not repeat the instructions.

Expected round A response (classification only):

```
@SESSION 7KQX2M
cat|conf|regen|del|ids
Node.js dependency folders|high|yes|1|3-33
Build outputs|med|yes|1|34,40-42
@END 7KQX2M
```

- conf: high|med|low. regen: yes|partially|no. del: 1|0.
- ids: comma-separated integers and inclusive ranges.
- Any ID not mentioned is "unknown"; there is no unknown list.
- '|' cannot occur in Windows file names, but category names come from the AI, so split
  each row from the right (maximum 4 splits).

Parser (tolerant on input, strict on validation):

- Extract the text between @SESSION and @END, ignoring preamble, postamble, and markdown
  fences. Normalize smart quotes, zero-width characters, non-breaking spaces, stray
  blockquote markers, and trailing whitespace. Headers are case-insensitive.
- Errors to detect: wrong or missing session code; missing @END (truncated); unknown enum
  values; IDs not in this batch; an ID claimed by more than one category; malformed ranges.
- On errors, generate a repair prompt listing each error concisely and asking for the full
  corrected response only.

### Phase 4: results and lazy explanations (round B)

- Results screen per the design: categories from built-in recipes and from the AI, merged
  and sorted by reclaimable size, with real names restored for display.
- Explanations for AI categories are lazy: "Request explanation" queues a category; the
  next export becomes a round B prompt listing queued categories by number, name, and IDs.
- Expected round B response:

  ```
  @SESSION 7KQX2M
  #2
  what: Packages installed by npm for JavaScript projects.
  lost: Nothing permanent; restored by running npm install.
  step: Open the project folder in a terminal.
  step: Run npm prune, or delete the folder and reinstall later.
  @END 7KQX2M
  ```

  Cap each field at about 25 words (state this in the prompt).
- Low-confidence categories keep the delete action disabled until the user has expanded
  the row.

### Phase 5: deletion pipeline

- Confirmation screen per the design; re-verify every item (safety rule 6) and show
  per-item status.
- Recycle Bin by default via the shell API; permanent delete behind an extra
  confirmation. Handle locked files and partial failures, and report exactly what was and
  was not deleted.
- Built-in recipes with brute-force disallowed expose only their managed steps.

### Phase 6: history, polish, packaging

- Session history (scan dates, space reclaimed), stored locally.
- Keyboard shortcuts, light and dark themes, all design states (first run, partial scan,
  parse errors, nothing found, partial deletion failure, invalid or expired session link).
- Copy buttons use the Clipboard API (localhost is a secure context), with a select-all
  fallback.
- Single-executable build, a Start menu shortcut that launches the app and opens the
  tokenized URL, and a README.

### Required tests (beyond per-phase unit tests)

- Scanner fixtures in a temp directory: hard links, junctions, symlinks, sparse and
  compressed files, long paths, access-denied folders.
- Redaction leak test: for every redacted item, assert that no generated prompt contains
  the original name (case-insensitive substring), including via descendants and
  username/machine name.
- Parser fixtures: preamble/postamble, fences, smart quotes, truncation, wrong session, bad
  enums, overlapping IDs, unknown IDs, pipes inside category names.
- Prompt injection: file names containing instructions must not cause any action outside
  the closed vocabulary.
- Deletion refuses IDs not in the scan, System-tier items, protected paths, and items
  changed since the scan.
- Localhost security: missing or wrong token, wrong Host, wrong Origin, and
  state-changing GET requests are all rejected.
- A dev-only script that estimates token counts for generated prompts on a sample scan
  (offline approximation is acceptable; it must not be shipped or make network calls).

---

## Part 2: Amendments

### A-1 (after Phase 0): Linux later

Build the architecture for Windows now, but make no decision that would make a later
Linux port difficult; Linux support is a likely future demand.

### A-2 (after Phase 1): categories are not limited to recipes; any kind of user

Categories must not be limited to hardcoded recipes. Detecting and explaining categories
dynamically is a main reason the project uses AI. Ullage must handle content beyond a
developer's workstation and work for every kind of user: video editors, lawyers,
streamers, or plain regular users.

### A-3 (after Phase 1): dates and other metadata in prompts

Modification and creation dates (and other metadata) help the AI judge how stale files
are. Include them, at month resolution (`YYYY-MM`) to save tokens. For redacted items,
redact their metadata entirely.

### A-4 (during Phase 2): documentation folder

Keep `ARCHITECTURE.md` in `docs/`, together with the plans and decisions reached in the
conversation, in appropriate Markdown files.

### A-5 (Phase 2 review): redaction scope, extensions, small files

- Redacted items keep their rounded size; all other metadata is dropped (D-030).
- A redaction affects only what its mechanism selects: an item picked in the tree
  affects that item (and its subtree); a glob or name pattern affects what it matches.
  There is no implicit hiding of similar names elsewhere (D-027). The leak test's
  "including via descendants" is checked within each redacted item's subtree.
- Extensions do not need to be hidden or redacted (D-031).
- Small files never reach the prompt individually. Only a folder holding a collection of
  small files large enough to be worth examining appears, as one line (D-032; Phase 3).

### A-6 (after Phase 6): explanations in the reply, six steps, brute force with warnings

- Explanations are not requested per category. They are an integral part of the export
  prompt's reply, even if that means more batches. This replaces the lazy round B in
  Phase 4 (D-066, D-069).
- The Results step is removed as a separate step: after "Paste response" comes "Delete",
  which shows the categories with their explanations and then checks, confirms and
  deletes on the same page (D-067).
- Recipes whose owning tool may already be uninstalled (for example Rust toolchains)
  allow brute-force removal, with a ⚠ warning explaining what it may break. This
  replaces "managed only" for the Docker Desktop disk image (D-068).

### A-7 (after Phase 6): changing folders, old files, and a clearer redaction review

- Folders that programs write to constantly (%TEMP%) are deleted entry by entry, so
  changes in some entries do not block the rest (D-070).
- An easy way to delete what has not changed in a long time: an age filter in the
  Delete step (D-071).
- Items that changed since the scan can be re-scanned from the Delete step and
  confirmed at their new size (D-072).
- Categories the AI suggests keeping are always listed (D-073); "Delete anyway" with a
  typed confirmation makes one selectable (D-078).
- The redaction review previews how the selected item reads in the copied prompt; a
  rule's full match list can be opened (D-074).
- A home folder hidden as a whole tree still reads `~` and, with well-known names kept,
  splits along the standard profile folders instead of becoming one size-only line
  (D-075).

