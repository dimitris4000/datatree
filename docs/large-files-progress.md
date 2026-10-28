# Large-file implementation ledger

Plan: docs/superpowers/plans/2026-10-27-large-files.md

Baseline: Go suite and 78 JavaScript tests passed during planning. state.json has 7,654,398 bytes, 167,455 lines, 137,636 tree nodes, depth 14. Fold scan measured ~156 ms in Node; 167,455 DOM rows per eager fold view. No browser timing established.

Ruling: work in the supplied temporary checkout, preserving the pre-existing edits in tree.js, tree.test.js and docs/implementation.md. No commits or branch changes are required for implementation.

Pre-flight: backend response contracts are shared by server and editor tasks; fixed above before dispatch. Document changes preserve current Session/Document APIs. Benchmark code is owned by the coordinator. Parallel workers own disjoint files.

## Implemented and checked

- Deterministic fixture writer, parser/source/search/edit benchmarks and opt-in real-handler acceptance workflow; private input remains outside the repository.
- CodeMirror 6 viewport editor, composed source patches, bounded lazy inspector, explicit full-copy/edit actions, indexed navigation and paginated Find. Local asset bundle and dependency licenses are reproducible.
- Metadata-only state, revision-scoped source/representation/location APIs, optimistic per-session commits and bounded asynchronous jobs with cancellation.
- Context-aware parsing/search, direct strict JSON scanner, captured spans, per-revision source indexes and safe same-span JSON/JSONL scalar tree sharing. General mutations retain a validated full-parse fallback.
- Persistent balanced text pieces with byte/UTF-16/newline metrics, changed-text history budget, immutable undo/redo and UTF-16 patch application. Evicted history slots are cleared to release references.

Ruling: 64 MiB history accounting covers changed text in retained transitions, excluding shared original/current source — summing full source lengths would make every >64 MiB document lose all Undo immediately. Whole-document replacements can still exceed the history budget.

Ruling: unchanged source geometry permits scalar tree sharing only when byte, raw-rune and UTF-16 lengths match and lexical whitespace is unchanged; structural updates use the full parser until the compact engine is ready. Cost: arbitrary edits still scale with file size.

## Evidence

- Initial backend sample baseline: parse 1.323 s /909 MB allocated; scalar replacement 1.462 s /1063 MB. Intermediate optimized sample: parse 0.495 s /172 MB; replacement 0.450 s /180 MB. Cumulative allocation is not peak RSS; final scanner/same-span measurements follow separately.
- Generated 10 MiB JSON/JSONL/YAML/CSV real-handler workflows passed, including exact-byte Undo.
- Initial 100 MiB JSON, JSONL and YAML workflows passed functionally, but several timing targets failed: JSON open9.44 s, edit7.25 s; JSONL open7.74 s, edit6.38 s; YAML open14.07 s, edit15.17 s. These predate final scanner/same-span optimization. CSV preview exposed a conversion-output budget issue under correction.
- Go suite and race suite passed before the final scanner edits; final checks must be rerun after those changes. JS suite passed98 tests before the final Original-tab synchronization regression was added.
- Independent review found post-commit cancellation stale-state recovery and full/truncated-copy problems. Frontend regressions reproduced them before fixes; updated report lists the passing evidence.

## Earlier qualification boundary (superseded below)

At that checkpoint, no native browser surface is available. Native IME, paint/scroll p95 and peak-memory qualification cannot be established here. Defaults remain20 MiB /one million nodes. The compact record engine and arbitrary incremental subtree parsing/index updates are not complete; this delivery must not be represented as full100 MiB latency qualification. No commit, merge or publication has occurred.


## Resumed implementation, 2026-10-28

- Compact records, anchored source navigation, local scalar/context edits and persistent compact Undo/Redo are integrated. All four generated 100 MiB in-process HTTP workflows passed; see compact-adapter-report.md for times and fallback boundaries.
- Tracked Inspector writes and post-commit cancellation recovery are fixed; frontend suite contains 102 tests.
- Playwright/Chromium qualification is now available through `npm run test:browser`. Private-file checks do not commit edits or capture screenshots/traces. The local example opened/rendered in 1.358 s with typing event p95 32 ms and 74 rendered lines.
- Latest full Go suite passed before startup profiling improvements. Final suite/race/vet/build checks follow qualification.
- Initial JSON/JSONL/YAML compact imports now emit records while walking the validated parser tree, avoiding a complete application Node graph and eager path/location strings. CSV still uses temporary legacy compaction. All formats still allocate a temporary full parser tree; peak RSS remains unqualified.
- Profiling-driven startup improvements preallocate record inputs once and use direct indexing for consecutive import IDs, retaining hash-map validation for sparse/reordered IDs. Small-fixture parity tests cover exact records, Unicode/CRLF, YAML aliases/comments, complex keys, streams and block scalars. Independent read-only review found no high-impact issue in these changes.

Ruling: default limits remain unchanged until memory and manual interaction qualification is complete. No commit, push or publication is authorized or performed. Existing unrelated pagination changes remain preserved.

- Browser qualification exposed a click-loss race during canonical refresh: updateControls re-enabled Undo/Apply before the restored inspector finished. A delayed-inspector regression failed first, then passed after busy-state guards were preserved. The frontend bundle was rebuilt; frontend suite now contains 103 tests.

Ruling: automatic compact storage now starts above 1 MiB, independently of the unchanged 20 MiB file limit. The 10 MiB YAML browser check exposed a 1.366 s legacy scalar commit below the old storage threshold; a megabyte-YAML regression failed on the legacy path before lowering the threshold. This gives medium-sized YAML/CSV the same local edit engine as 100 MiB inputs. Cost: more imports pay compact-construction overhead; the browser matrix qualifies that tradeoff.

## Qualification checkpoint

- All four 10 MiB and 50 MiB Chromium strict checks passed after the compact-threshold and refresh fixes. Latest private example: 1.256 s open/render, typing p95 32 ms, 74 rendered lines.
- 100 MiB browser workflows completed across formats. Recorded strict passes: JSON 7.167 s, JSONL 7.392 s, CSV 9.598 s open. YAML latest 10.719 s missed the 10 s open gate, while typing p95 32 ms, reveal188 ms and edit250 ms passed. Full numeric table and timing caveats are in large-files-performance.md.
- Initial generated100 MiB YAML process benchmark: parse7.950 s, cumulative allocation2.464 GB, process peak RSS2.094 GB including fixture/runtime. Initial transient memory remains a material limitation.
- Final Go, race, vet and JS runs are in progress. Remaining qualification is explicit; no claim of universal100 MiB instant operation or full memory qualification is warranted.

- Local file reads now preallocate from the Stat size hint while still reading through the configured limit plus one overflow byte. Regression coverage checks files growing/shrinking after Stat and oversized input. Latest100 MiB YAML browser run:10.394 s open, typing32 ms, reveal173 ms, edit254 ms; only the open gate fails.
- Reproducible asset rebuild matched SHA-256 `2a617aafaf116388b01710a57a810e3738c1f5090a0a5dfe046331e1bee37aa8`; license notices matched `ff894b1297e561566d82e7685cd05277c6d4f67907308633df34e270b7bd8208`.

## Final verification for this continuation

- `GOCACHE=/tmp/datatree-go-cache go test ./... -count=1`: passed.
- `GOCACHE=/tmp/datatree-go-cache go test -race ./... -count=1`: passed.
- `GOCACHE=/tmp/datatree-go-cache go vet ./...`: passed.
- `npm test`:103/103 passed.
- `npm run build:web`: passed and reproduced the recorded bundle/license hashes.
- `git diff --check`: passed.
- Current preview uses the updated embedded bundle and private startup file at http://127.0.0.1:8091 (Go exec session92922). The private file was not overwritten.

No interrupted verification commands remain. Further work is the explicitly missed YAML100 MiB initial-open target, parser-to-record streaming to reduce initial transient memory, and manual IME/accessibility/cached-navigation qualification. Full100 MiB instant-operation qualification is not complete. All implementation changes remain uncommitted in the supplied checkout; preserve the pre-existing pagination edits.

## Authorized checkpoint and memory follow-up

The user authorized a commit of the large-file implementation and continued work. The measured100 MiB YAML startup time is accepted for now; further effort targets initial transient memory and remaining operation/manual qualification. Pre-existing pagination changes and the separately supplied AGENTS.md remain outside the commit. The staged frontend bundle is built against the staged tree source, preserving working pagination changes separately.

## Memory follow-up after c59712f

Plan: (1) postorder compact builder with equivalent hierarchy/span/limit/cancellation validation; (2) emit JSON/JSONL/YAML records during traversal and release consumed raw references; (3) stream CSV rows directly into records; (4) compare generated100 MiB allocations/RSS, run full/race suites and browser workflows. The accepted YAML startup time is not a blocker.

Builder tests failed for missing NewBuilder before implementation, then passed. Invalid hierarchy, repeated child, unreachable record, depth/weight, missing alias and cancellation cases reject publication. Finished builders cannot mutate published versions. Existing compact record parity tests pass after switching the importer. Initial YAML measurement: cumulative allocation2.198 GB versus2.464 GB; process peak RSS1.926 GB versus2.094 GB. CSV streaming parity tests failed for missing parseCompactCSV before implementation; they cover CRLF, quoted newlines/escaped quotes, Unicode, empty cells/records, header-only files and header storage reuse.

- CSV streaming100 MiB: allocation445,199,352 bytes, process peak RSS612,114,432 bytes, parse1.188 s. Isolated checkpoint baseline rerun: allocation3,566,269,552 bytes, peak RSS2,941,165,568 bytes, parse5.981 s.
- Chromium100 MiB CSV:3.696 s open,32 ms typing p95,41.5 ms cached navigation,227 ms edit. JSON:7.389 s open,40 ms typing,43.5 ms cached navigation,235 ms edit. YAML with explicitly accepted12 s startup budget:10.735 s open,32 ms typing,31.5 ms cached navigation,213 ms edit. JSONL:7.810 s open,32 ms typing,31.2 ms cached navigation,219 ms edit.
- Added Chromium protocol IME composition with emoji, accessible labels, Escape then Tab focus exit and keyboard draft discard. These pass on100 MiB fixtures; native OS IME UI and screen-reader use remain manual. Cached navigation additionally asserts zero repeat source-location fetches.
- Read-only source review found no confirmed correctness regression. Additional parity cases now cover recursive YAML aliases and empty YAML; builder tests cover exact scalar depth/weight and cancelled Finish.
- Initial checkpoint c59712f is committed. The separate contributor-guide commit2e09c99 appeared during continuation and remains preserved. Follow-up memory changes remain uncommitted.

Memory follow-up verification: full Go suite, full race suite, go vet, all103 Node tests, asset rebuild and git diff --check passed. Updated Chromium100 MiB workflows passed for JSON, JSONL and CSV with original strict budgets, and YAML with the explicit accepted12 s open budget; composition, keyboard focus exit/discard and cached navigation checks pass. No interrupted commands remain. Follow-up code is uncommitted; c59712f remains the requested implementation checkpoint. Native OS IME/screen-reader qualification and further JSON/JSONL/YAML parser streaming remain separate work. Existing pagination changes and their derived working bundle difference remain preserved.
