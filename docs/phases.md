# Phase plan

Each phase ends with a summary (what was built, how it was tested, deviations) and a stop
for review. Status is updated as phases land.

| Phase | Deliverable | Test focus | Status |
|-------|-------------|------------|--------|
| 0 | Stack, architecture, plan | — | Done, approved |
| 1 | Scanner, tiers, recipes, hardened server, Scan and Overview screens | Scanner fixtures, localhost security, no-network checks | Done, reviewed |
| 2 | Redaction engine, encrypted rules store, redaction review and rules screens; dates, file types and non-developer recipes | Leak test, fail-closed test, rename-following | In progress |
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

## Phase 2 scope

- Scanner records creation time and per-folder file-type mix (D-013).
- Additional non-developer recipes (D-012).
- Shared path-pattern package used by tiers and redaction.
- Secret storage: DPAPI on Windows, a key-file backend on Linux; fail closed.
- Redaction engine: rule types exact, subtree, glob and name-anywhere; treatments whole
  subtree and name-keep-extension; file-ID anchoring that follows renames; always-on
  redactions (D-025 to D-030).
- Redaction review screen (step 3) and rules manager screen with match counts.
- Leak test over every rendered path, including descendants, usernames and machine name.
