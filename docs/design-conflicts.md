# Design export versus specification

The design export (Claude Design, "Ullage Screens") is followed for layout, components and
states. Where it disagrees with [requirements.md](requirements.md), the requirements win.
Each conflict below was reported when found.

| # | Design export | Requirement | Resolution | Found in |
|---|---------------|-------------|------------|----------|
| 1 | Loads IBM Plex from `fonts.googleapis.com` | Safety rule 1: no remote assets | Plex `.woff2` files bundled under the OFL | Phase 0 |
| 2 | `support.js` loads React from `unpkg.com` | Safety rule 1 | Design runtime not shipped; Preact bundled | Phase 0 |
| 3 | Placeholders shown as `<REDACTED_3>` | Placeholders are `<R{n}>` | `<R3>`, `<R3>.ext` | Phase 0 |
| 4 | Session code `7F3K-Q2MD` | `@SESSION 7KQX2M` | 6 characters from an unambiguous alphabet | Phase 0 |
| 5 | Prompt asks for a fenced JSON reply ("Schema ullage/v1") | Pipe table between `@SESSION` and `@END` | Pipe format | Phase 0 |
| 6 | Repair prompt asks to resend only the broken categories | Ask for the full corrected response only | Full response; the UI still shows which rows were valid | Phase 0 |
| 7 | Item IDs like `i713` | Plain integers and ranges | Plain integers | Phase 0 |
| 8 | Partial scan shows an "Unreadable" tier | Every scanned byte in exactly one of three tiers | "Unaccounted" band (volume used minus scanned), not a tier | Phase 0 |
| 9 | pagefile.sys, hiberfil.sys and WinSxS listed under System | They are required Known recipes | Known tier | Phase 1 |
| 10 | Overview says "Last-modified month" leaves the machine | Not in the Phase 3 prompt format at the time | Removed in Phase 1, restored as amendment A-3 added dates | Phase 1 |
| 11 | Rules file `%LOCALAPPDATA%\Ullage\rules.json` | DPAPI-encrypted in the config directory | `%APPDATA%\Ullage\rules.dpapi` | Phase 2 |
| 12 | Keyboard: `1-7` steps, `R`, `H`, `T`, `j/k`, `x`, `d` | Not specified | Adopted as designed | Phase 0 |
