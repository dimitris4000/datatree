# Structural Diff Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to execute task-by-task with failing tests before implementation.

**Goal:** Compare the applied document with its original or a selected file and navigate structural changes.
**Architecture:** A bounded Go AST comparator returns full counts/highlights and up to 200 change previews. Session API retains one comparison file and original format. A focused frontend module owns the Changes panel; app.js reuses guarded reveal navigation and tree.js renders explicit diff badges.
**Tech Stack:** Existing Go/YAML AST and native JavaScript; no dependencies.
**Spec:** docs/superpowers/specs/2026-10-27-structural-diff-design.md

## Global Constraints

10 MiB input, existing node/depth limits, index-based arrays, key-based mappings, no alias expansion, exact numeric comparisons, 200 displayed changes with complete counts/highlights or explicit work error. No mutation/history/disk changes. Preserve current working changes.

## Review Focus

- Removed nodes must navigate to the surviving parent, never reused numeric IDs from the baseline.
- Original format must survive conversions and history transitions.
- Complex keys, reordered maps and renamed anchors must compare by structural meaning.
- Failed comparison uploads must preserve the prior baseline/results.
- Late responses must not restore hidden highlights or stale document IDs.

## Task 1: Comparator
Files: internal/document/diff.go, diff_identity.go, diff_test.go; session.go for OriginalFormat.
Interfaces: Diff(base, working *Document) (DiffResult,error); DiffResult{Changes,Counts,Marks,Truncated}; change{Status,Before,After,Target}; marks map current node IDs to added/changed/contains flags.
- [x] Write and run failing tests for nested changes, collection summaries, ordering, exact numbers, YAML tags/aliases/streams/complex keys, bounds and complete counts.
- [x] Implement bounded structural fingerprints, comparison and ancestor marks. Reuse exact numeric normalization.
- [x] Add failing session tests for original-format retention; implement and run document tests.

## Task 2: Session API
Files: internal/server/diff.go, diff_test.go; server.go integration.
Interfaces: GET /api/diff?revision=N&baseline=original|file; POST /api/diff-baseline {revision,format,text,filename} validates and compares before atomically replacing session baseline. Responses contain revision, baseline metadata and DiffResult.
- [x] Test missing/stale revision, isolation, immutable working state/history, bad baseline retention, import reset, conversion and original comparison.
- [x] Implement handlers with existing origin, size and session protections; reset extra baseline only after successful working import/open. Run HTTP tests.

## Task 3: Panel, navigation and tree badges
Files: internal/server/web/diff.js, diff.test.js; app.js, tree.js, index.html, style.css.
Interfaces: DiffController loads/refreshes/selects baseline; mountDiffPanel renders text-only changes; shared navigateNode reveals current Target IDs through existing source-map hierarchy and draft guards.
- [x] Test stale refreshes/uploads, failures preserving results, baseline selection, reset/close, navigation cancellation, safe previews and renderer badges.
- [x] Implement the panel and reuse shared reveal navigation. Refresh on revision installation while open, hide marks on close, show full counts/truncation and textual added/changed/contains labels.
- [x] Run JavaScript suite and syntax checks.

## Task 4: Review and verification
- [x] Update README and implementation records with semantic rules and actual limits.
- [x] Run go test ./..., go test -race ./..., go vet ./..., npm test and diff checks.
- [x] Review against approved design; resolve material findings with regressions. Report browser verification boundary. No commit/publish requested.

## Execution notes
Proceed inline under the explicit instruction to implement. Keep this plan as the progress ledger. Current checkout is the isolated temporary workspace; prior feature changes are already present in HEAD. No branch/push action is necessary.

## Verification record

- Document regression tests were observed failing before implementation; session API tests returned 404 before handlers; panel/controller tests failed before the module and canvas integration existed.
- Final verification passed: GOCACHE=/tmp/datatree-go-cache go test ./..., go test -race ./..., go vet ./..., npm test (51 passing), JavaScript syntax checks and git diff --check.
- Review identified cached temporary identities leaking into an external YAML alias key. A failing regression reproduced the missing addition/removal. Scoped complex-key reference identities fixed it; review confirmed the repro and additional cyclic/reordered-key probes pass with no further material findings.
- Original-format retention, failed baseline replacement, immutability, complete counts beyond the display cap, revision/session isolation, safe previews, serialized uploads and close/stale-response behavior are covered.
- Shared navigation retains the existing source-draft and inspector-draft guards, source-map page reveal and tree rendering cap.
- Browser inventory reports no connected apps or browsers. Live visual/browser acceptance remains unverified.
- No commit, push or publication requested or performed.
