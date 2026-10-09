# Phase plan

Each phase ends with a summary (what was built, how it was tested, deviations) and a stop
for review. Status is updated as phases land.

| Phase | Deliverable | Test focus | Status |
|-------|-------------|------------|--------|
| 0 | Stack, architecture, plan | — | Done, approved |
| 1 | Scanner, tiers, recipes, hardened server, Scan and Overview screens | Scanner fixtures, localhost security, no-network checks | Done, reviewed |
| 2 | Redaction engine, encrypted rules store, redaction review and rules screens; dates, file types and non-developer recipes | Leak test, fail-closed test, rename-following | Done, approved |
| 3 | Prompt builder, parser, validator, repair prompts, Export and Paste screens | Parser fixtures, injection, token estimator | Done, approved |
| 4 | Results screen, round B explanations, low-confidence gating | Merge and sort, round B parser | Done, approved |
| 5 | Deletion pipeline and confirmation screen | Guard refusals, change detection, partial failures | Done, approved |
| 6 | History, all design states, packaging, Start menu shortcut, README | End-to-end on Windows CI | Done, approved |

## Phase 0 summary

Chose Go + Preact (decisions D-001 to D-003), wrote the architecture, and listed the
conflicts between the design export and the spec. Approved with one amendment (A-1: keep
a future Linux port cheap).

## Phase 1 summary

Built:

- Portable parallel scanner: allocated sizes (compressed, sparse, WOF, dedup), hard links
  counted once with a deterministic owner, links recorded and never followed, cloud
  placeholders counted as not stored locally, access-denied folders recorded, partial
  marking, pause, resume and cancel, live progress over Server-Sent Events.
- Tier classification during the walk; 22 recipe files plus System-tier rules;
  installed-application claims with a breadth guard.
- Hardened local server (decision D-005) and the no-network enforcement (D-023).
- Scan, Scanning and Overview screens in light and dark themes.

Tested with fake-filesystem unit tests, real-filesystem fixtures on Linux and on Windows
CI (junctions, symlinks, hard links, sparse, NTFS compression, long paths, ACL-denied
folders, offline attribute), localhost security tests, the no-network check, and a
headless Chromium run with no console errors, CSP violations or remote requests.

Deviations reported: D-004, D-008, D-010, D-015, D-019, D-020, D-022, D-024.

Feedback received: amendments A-2 (AI-first categories, any kind of user) and A-3 (dates
in prompts, no metadata for redacted items), folded into Phase 2.

## Phase 2 summary

Built:

- Scanner records creation time and each folder's three largest file types (D-013).
- Ten more recipes for everyday users: web browsers, chat apps, Adobe media cache, GPU
  shader caches, phone backups, crash reports, Windows Update downloads, thumbnails and
  Spotify (D-012). 32 recipe files in total.
- `pathpattern`: the glob engine, now shared by tiers, scan exclusions and redaction.
- `protect`: DPAPI for the current user on Windows, a key-file backend on Linux; every
  failure is an error.
- `redact`: rule types exact, subtree, glob and name-anywhere; treatments whole subtree
  (with the well-known-names option) and keep extension; file-ID anchors that follow
  renames; always-on redaction of the profile (~), account names, machine name and SIDs;
  placeholders persisted encrypted (D-025 to D-031, D-034). After review, a redaction
  affects only what its mechanism selects (D-027).
- Session and API: redaction summary, lazily loaded review tree, item detail with a
  prompt preview, session overrides, rules with match counts, fail-closed handling of an
  unreadable rules file (D-033).
- Screens: Redaction review (step 3) and the Redaction rules library, per the design.

Tested:

- Required leak test: randomized trees (5 seeds, 1,500 files each) with redacted names,
  user and machine names, SIDs and short names; asserts no redacted name appears in its
  own path or its descendants', and no account name, machine name or SID appears
  anywhere. Verified to fail when subtree hiding or the always-on check is disabled.
- Rule semantics, session overrides, rename-following, placeholder stability, sanitizing
  of names that imitate placeholders, short-word matching.
- Encrypted store: round trip, no plaintext on disk, fail closed on tampered, foreign,
  empty, truncated or invalid files; the file is never overwritten while unreadable.
- DPAPI round trip and tamper detection on Windows CI.
- Session-level tests for rules CRUD, persistence across launches and stale scan codes;
  server tests for the new endpoints' method rules and the reset confirmation.
- Headless Chromium run of the review and rules screens: no console errors, CSP
  violations or remote requests.

Deviations reported: D-026 to D-034; design conflicts 11, 13 and 14.

Review outcome: D-030 and D-032 approved; D-027 and D-031 revised and implemented
(amendment A-5); D-028 approved.

## Phase 2 scope (as planned)

- Scanner records creation time and per-folder file-type mix (D-013).
- Additional non-developer recipes (D-012).
- Shared path-pattern package used by tiers and redaction.
- Secret storage: DPAPI on Windows, a key-file backend on Linux; fail closed.
- Redaction engine: rule types exact, subtree, glob and name-anywhere; treatments whole
  subtree and name-keep-extension; file-ID anchoring that follows renames; always-on
  redactions (D-025 to D-030).
- Redaction review screen (step 3) and rules manager screen with match counts.
- Leak test over every rendered path, including descendants, usernames and machine name.

## Phase 3 summary

Built:

- `prompt`: item selection (D-035), line notation and layout (D-036), batching with
  general-audience instructions, exactly one worked example and short continuation
  prompts for later batches.
- `response`: tolerant extraction and normalization, strict validation, repair prompts
  that ask for the full corrected reply (D-039).
- Session and API: exports built on demand, failing closed while rules are unreadable;
  new session code on rebuild (D-038); paste and import per batch.
- Screens: Export prompt (step 4) with Clipboard API copy and select-all fallback, and
  Paste response (step 5) with the checklist, problems, repair prompt and batch status.
- `tools/tokenestimate`: dev-only offline estimate on a synthetic mixed PC or a real
  folder (D-042).

Tested:

- Parser fixtures for every case the spec lists: preamble and postamble, fences, smart
  quotes, truncation, wrong session, bad enums, overlapping IDs, unknown IDs, pipes in
  category names, plus malformed and huge ranges, Markdown tables, bold markers and a
  prompt pasted before the reply.
- Prompt injection: hostile file names stay inside one data line and cannot open or close
  a block or add columns; hostile replies cannot reference anything but enums and IDs of
  the batch. Found and fixed a bidi-override spoofing gap (D-040).
- Prompt-level leak test: redacted names, names inside redacted folders, the user name,
  the machine name and SIDs never appear in any batch.
- Purity (no ID covers Known or System bytes), budget, batching, determinism, the worked
  example parses, redacted items carry only their size, content-aware rollup.
- Session tests for the export lifecycle and fail-closed behavior; a headless Chromium run
  of export, copy, a rejected paste and a successful import.

Deviations reported: D-035 to D-042. Review outcome: approved; no enum synonyms (D-039).

## Phase 4 summary

Built:

- `results`: recipe categories (grouped per recipe, nested claims counted once,
  installed apps and small categories left out) merged with AI categories, sorted by
  reclaimable size; `del 0` rows kept aside; a summary of everything not listed (D-043).
  Real names restored locally from the export's own placeholders (D-047).
- Round B: a queue of AI categories, the round B prompt (numbers, the assistant's names,
  IDs, the 25-word cap stated), its parser and repair prompt, and import (D-044, D-045).
- Low-confidence gating recorded by the server (D-048).
- Results screen (step 6) per the design: source, confidence and regenerable filters,
  expandable rows with explanation, managed steps, brute-force policy and the item list
  with real names; queued note linking to the export round; keys j / k / Enter / x.
  Export and Paste screens show the round B prompt as the step after the batches.

Tested:

- Merge and sort, nested claims, thresholds, installed apps, kept categories, summary
  totals, restoration, unknown IDs, gating and queue states (`internal/results`).
- Round B parser fixtures: the spec example, tolerant wrapping, wrong session,
  truncation, unrequested and duplicate numbers, missing fields, step limits, overlong
  and empty fields, a round A reply in the wrong slot, bidi and control characters.
- Session round trip: queue, round B prompt without redacted names and with every data
  line a `#n` line, invalid and valid replies, partial answers staying queued, re-import
  dropping stale explanations.
- Server method rules for the new endpoints; frontend selection and round helpers.
- Headless Chromium: import, Results, low-confidence unlock on expand, request, round B
  export and paste, explanation shown, dark and light themes; no console errors, CSP
  violations or remote requests.

Deviations reported: D-043 to D-050; design conflicts 15 to 18.

Review outcome: approved; D-050 resolved the strict way (an AI category needs its
explanation before it can be selected).

## Phase 5 summary

Built:

- `deletion`: the guard (policy, scan purity, protected locations, ancestor identity,
  re-walk fingerprint), deletion through the Recycle Bin or a permanent walk that never
  follows links, outcomes checked on disk, and a background run with stop
  (D-051 to D-056). Loose contents resolved (D-053).
- `platform`: `MoveToRecycleBin` (SHFileOperationW on Windows, freedesktop Trash on
  Linux), `CanonicalPath`, `ProtectedTrees`, in-use detection, and a flag for entries
  managed through non-link reparse points.
- Session and API: prepare (verify a selection), execute (typed confirmation for
  permanent), progress, stop; deleted items leave Results (D-057).
- Delete dialog (step 7) per the design: confirmation with per-item verification, Recycle
  Bin or permanent with typed confirmation, progress with stop, and the final report with
  copy error log and retry.

Tested:

- Required refusals: keys not in the current results (unknown category or item, another
  category's item, text instead of an ID), categories not allowed (kept, unexplained,
  managed only), System tier (also outside protected folders), the Windows folder and
  Program Files, profiles and their parents, scanned roots, items outside the roots,
  recipe folders holding other categories, excluded, unreadable, cloud-only and managed
  items.
- Changes since the scan: modified, file added or removed, link added, missing,
  replaced, a parent folder swapped for a link, and a change between confirmation and
  deletion; loose contents changes.
- Execution: recycle, permanent with typed confirmation, wrong or forged confirmations
  and plans, plan reuse, in-use failure with nothing changed, partial failure, stop after
  current item; links inside deleted folders keep their targets (real junctions on
  Windows CI, symbolic links on Linux); the real Recycle Bin and Trash.
- Mutation check: disabling the ancestor check or the System-tier check makes tests fail.
- Headless Chromium on a disposable copy of the fixture: select, confirm (one item
  blocked as changed), recycle, done, results refreshed, permanent delete with typed
  confirmation; no console errors or remote requests.

Deviations reported: D-051 to D-057; design conflicts 19 and 20.

Review outcome: approved. Cloud-synced and cloud-only items became deletable with clear
warnings instead of being refused (D-052).

## Phase 6 summary

Built:

- `history`: encrypted, path-free session history; History screen (key H) with totals,
  per-session rows and clear (D-059).
- `instance`: single instance through a per-user named pipe; a second launch opens a new
  tab of the running session (D-060).
- Start menu shortcut on first run and on request, written without COM or cgo (D-061).
- Release build: one GUI-subsystem executable, version stamped, message box for fatal
  errors, build scripts for bash and PowerShell (D-062). README rewritten for users.
- Design states audited (first run, scanning, partial scan, the three paste states,
  nothing found, deletion progress and partial failure, invalid link) and keys 1-7, R,
  H, T wired.
- Fixed along the way: a run was reported finished before its results were recorded;
  Linux reported a scanned folder as its drive name (history would have held a path);
  a file inside a cloud-sync folder was not flagged as cloud-synced.

Tested:

- History: persistence, ordering, clear, unreadable file, encryption, no paths (also
  for folder scans at the session level).
- Single instance (Windows CI): second claim refused, launch requests answered, no
  instance found; launch-link validation.
- Shortcut (Windows CI): Windows' own shell reads target, working folder and description
  back, with a non-ASCII folder name.
- API end-to-end over loopback on Windows and Linux CI: token required, scan, export,
  both rounds, verify, recycle, file kept that the AI said to keep, history (D-064).
- Headless Chromium: the full flow ending on the History screen; no console errors or
  remote requests.

Deviations reported: D-059 to D-064; design conflicts 21 and 22.

Review outcome: approved, with one change: logs are written, with sensitive information
redacted (D-063 revised).

## After Phase 6: explanations in the reply, six steps

Requested by the owner (amendment A-6): explanations are part of every batch reply
instead of a per-category round B; Results and Delete are one step; recipes whose
owning tool may be gone allow brute force with a ⚠ warning; batches are smaller.

Built:

- Parser: `what:`, `lost:` and `step:` lines below each row, found by prefix first;
  required for `del 1`; a new "Explanations" check with repair prompts (D-066).
- Prompt: instructions and the worked example carry explanation lines; default batch
  budget 150 data lines (D-069).
- Removed: round B queue, prompt, parser, `/api/results/explain`, and their screens.
- Step 6 "Delete": the category list with explanations inline, then verification,
  confirmation and progress on the same page (D-067).
- Recipes: `brute_force.warning`; seven recipes switched to brute force with a warning
  (D-068). The warning is shown on the row, in the detail, and on the confirmation.

Tested: parser fixtures for explanations (missing, duplicate, too many steps, before the
first row, `|` in the text, Markdown tables), the worked example parses with its
explanations, prompt and parser limits agree, recipe validation of warnings, results
carry warnings and inline explanations, session import with explanations and refusal of
an unexplained row, API end-to-end with explanations, and a headless Chromium run of
export, a refused unexplained reply, import, the Delete list, the Rust toolchains
warning and the confirmation.


## After Phase 6: changing folders, old files, re-scans, redaction preview

Requested by the owner (amendment A-7). Built: split entries for `%TEMP%` (D-070), the
"Unchanged for" age filter (D-071), re-scanning changed items from step 6 (D-072), kept
categories always listed (D-073), the real prompt lines in the redaction inspector and
the full match list of a rule (D-074).

Tested: split entries in results and in the deletion guard (a change in one entry
blocks only that entry), the age filter in results and the session, re-scan baselines
in the guard and the session (accepted, kept, blocked again by a later change), prompt
lines before and after a whole-tree redaction including the covering line, the rule
match list, and a headless Chromium run of all of them.
