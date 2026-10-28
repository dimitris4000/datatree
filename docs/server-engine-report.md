# Large-file server implementation evidence

## Implemented contracts

- State and mutation responses contain revision/history/format/tree/schema/filename/limits metadata and omit document source. Existing tests now obtain source explicitly.
- Revision-scoped `GET /api/source` returns normalized text with `X-Document-Revision`; optional UTF-16 slices reject malformed, reversed, out-of-range and split-surrogate boundaries. `original=true` selects the immutable imported bytes. Slices normalize only the requested byte span.
- `/api/locate` and `/api/node-location` return indexed ranges and root-to-parent ancestor pages (20 children/page).
- `/api/representation` supports native/JSON/YAML and explicit `full=true`. Default preview is at most 64 KiB, cut at UTF-8 boundaries, with `truncated` and `totalBytes`. Representations use `Document.RepresentationContext` with a bounded UTF-8 collector; native YAML preserves standalone dedented subtree semantics. JSONL/CSV native uses the existing JSON subtree editing representation.
- Inspector responses contain representation placeholders instead of eager JSON and YAML conversions. Large scalar controls are omitted; visible label/path/value metadata is bounded. Invalid submitted subtree drafts remain present and marked as full text.
- `/api/patch` validates ascending non-overlapping UTF-16 spans before atomic parsing/commit. Split-surrogate and invalid final-document edits leave revision/history untouched. Untouched CRLF bytes survive patches. The server delegates storage and parsing to `Session.Patch`, retaining persistent piece-tree/history sharing. Successful responses include `previousRevision` and the submitted `patches`.
- Find forwards request cancellation and cursor/limit to `Document.FindContext`, retaining legacy defaults.
- `/api/preview` conversion output defaults to 64 KiB with truncation metadata; `full:true` explicitly requests complete text.

## Concurrency and jobs

The global mutex now covers session registry lookup/eviction only. Each browser session has its own mutex. GET handlers operate on immutable snapshots outside session locks. Apply/patch/import/open/edit/history/diff-revert, and in-memory conversion compute on cloned session history outside locks, then compare revision, schema generation and comparison baseline before committing. Cancelled requests cannot commit after their cancellation check. Conversion preview and schema evaluation run outside session locks. Conversion with an exclusive local file save and comparison-baseline replacement remain serialized within their own session.

Original parsed diff baselines and baseline hashes are cached. Git provenance is captured when opening a local file and retained until reopening, rather than spawning Git processes on every state response. Startup sessions share immutable source/original data without copying whole files during registry lookup.

Optional asynchronous work:

- Add `?async=true` to supported mutation POSTs: import, open, apply, patch, edit, history, diff-revert, and conversion without `saveFilename`.
- Returns HTTP 202 `{job,status:"queued"}`. Poll `GET /api/jobs?id=ID` for `{id,status,stage,httpStatus,result,error}`. Complete `result` is the ordinary metadata response.
- `POST /api/jobs/cancel` with `{id}` requests cancellation. Cancellation and commit synchronize on the same session mutex. Already committed operations reject cancellation.
- Jobs are scoped to the browser session; at most two active per session, four globally, eight retained per session. An evicted session cannot be substituted as the owner of a worker request.
- Stages are queued, the operation name while running, cancelling, and terminal complete/failed/cancelled; percentage progress is not provided.

## Verification

RED: `go test ./internal/server -run 'TestLargeFileReadContracts|TestPatchAtomicUTF16|TestLazyInspectorAndRepresentations' -count=1` failed on source-in-state, unsupported patches, and eager inspector serialization before implementation. Async ownership/result test failed because ordinary mutation HTTP 200 was returned before the job wrapper existed.

GREEN (with `GOCACHE=/tmp/datatree-go-cache` because the normal build cache is sandbox restricted):

- `go test ./internal/server -count=1`: passed, including normalized Unicode/CRLF slices, preserved original/export bytes, patch failure atomicity, bounded native preview, lazy inspector, optimistic concurrent writes, async ownership/result and cancellation rollback.
- `go test ./... -count=1`: passed across cmd/document/perftest/server/textstore.
- `go test -race ./internal/server -count=1`: passed, including concurrent mutation and async cancellation tests.
- `go vet ./...`: passed.
- `git diff --check`: passed at the time checked.

## Remaining boundaries

Import/open/apply/patch/edit/history now forward request contexts into session/parser APIs, and representations forward contexts into streaming serialization. Cancellation remains cooperative at engine checkpoints; underlying YAML decoder calls and full conversion do not provide immediate interruption. Async stages provide activity and cancellation, not parser percentage progress. Local save conversion is deliberately synchronous to preserve exclusive creation and avoid reporting a cancelled operation after writing its file. Comparison baseline parsing stays serialized within its browser session; it never holds the global registry mutex.

JSON/YAML representation output streams through the bounded engine collector. Nonnative YAML still clones its syntax tree to normalize presentation styles. Explicit full conversion preview still computes its full intermediate before truncating its response. Source slices normalize only their selected span, but boundary lookup and byte counting remain linear in the relevant source span. No 100 MiB acceptance or browser responsiveness claim follows from these backend tests. Current default document limits remain unchanged.
