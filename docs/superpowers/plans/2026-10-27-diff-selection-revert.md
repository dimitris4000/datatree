# Diff selection and revert implementation

User requirements: selectable Before/After values and a Revert action on every diff entry.

Implementation decisions: render preview text in ordinary selectable preformatted blocks outside navigation buttons. Revert applies exactly one server-recomputed inverse change to the working document, retaining the selected baseline. Additions are deleted, removals restored at their original collection position, and changes replaced by baseline content. Each accepted revert is one undoable revision. Retain format, draft guards and atomic validation. Removed array positions beyond the current tail require earlier positions restored first. Never reconstruct edits from truncated previews.

Baseline source tokens plus document revisions prevent applying a stale diff after another comparison-file selection. YAML edits preserve bytes outside the affected source span; references must resolve to compatible current targets, and dangling/unrepresentable results fail without mutation.

- [x] Write failing document tests for inverse operations, nested/root/type changes, history, collection order, YAML references/complex keys and rejected inverses.
- [x] Implement server-owned change references, bounded grafting and source-preserving serialization; session commits only validated results.
- [x] Test and implement revision/token-checked revert API with unchanged baseline and atomic errors.
- [x] Test and implement selectable preview DOM, separate Navigate/Revert controls, draft/stale guards and refresh after successful mutation.
- [x] Update README/implementation notes, run full Go/race/vet/JavaScript checks, and obtain read-only code review.

Preserve existing uncommitted structural-diff work. No commit or publication requested.

Validation: new document and API tests were observed failing before production changes. Frontend tests reproduced values nested in a button and the missing Revert operation. Final go test ./..., go test -race ./..., go vet ./..., npm test (53 tests), syntax checks and git diff --check passed. Read-only review found no material correctness issues. Browser visual/text-selection verification remains unavailable. Existing structural-diff work was preserved; no commit or publication performed.
