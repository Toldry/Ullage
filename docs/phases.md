# Phase plan

Each phase ends with a summary (what was built, how it was tested, deviations) and a stop
for review. Status is updated as phases land.

| Phase | Deliverable | Test focus | Status |
|-------|-------------|------------|--------|
| 0 | Stack, architecture, plan | — | Done, approved |
| 1 | Scanner, tiers, recipes, hardened server, Scan and Overview screens | Scanner fixtures, localhost security, no-network checks | Done, reviewed |
| 2 | Redaction engine, encrypted rules store, redaction review and rules screens; dates, file types and non-developer recipes | Leak test, fail-closed test, rename-following | Done, awaiting review |
| 3 | Prompt builder, parser, validator, repair prompts, Export and Paste screens | Parser fixtures, injection, token estimator | Planned |
| 4 | Results screen, round B explanations, low-confidence gating | Merge and sort, round B parser | Planned |
| 5 | Deletion pipeline and confirmation screen | Guard refusals, change detection, partial failures | Planned |
| 6 | History, all design states, packaging, Start menu shortcut, README | End-to-end on Windows CI | Planned |

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
  redacted names hidden wherever they occur; placeholders persisted encrypted
  (D-025 to D-031, D-034). Aho-Corasick matching keeps propagation linear.
- Session and API: redaction summary, lazily loaded review tree, item detail with a
  prompt preview, session overrides, rules with match counts, fail-closed handling of an
  unreadable rules file (D-033).
- Screens: Redaction review (step 3) and the Redaction rules library, per the design.

Tested:

- Required leak test: randomized trees (5 seeds, 1,500 files each) with redacted names,
  user and machine names, SIDs and short names; asserts no rendered path contains a
  redacted name, a hidden name inside its subtree, an account name or the machine name.
  Verified to fail when propagation or the always-on check is disabled.
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
