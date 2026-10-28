# Large-file frontend implementation evidence

The source pane now uses CodeMirror 6 with a viewport-bounded DOM and composed change sets. Typing and selection do not serialize or scan the complete source. Apply sends normalized UTF-16 patches against the applied revision; a successful source Apply reuses the already-known editor document. Import, source replacement, copying, and explicit full subtree submission may serialize complete text.

Ordinary startup reads metadata, one revision-scoped source response, and one root tree page. It does not request `/api/source-map`, original source, an inspector, or representations. Original source is fetched on demand and cached for the current imported document. Navigation uses `/api/locate` and `/api/node-location` with paged ancestor chains. Loaded expansion and selection paths are restored across same-document revisions while retaining canvas pan/zoom; structural changes can make an old index path refer to a different entry.

Inspector representations load only inside open details. Preview editors remain read-only even when their response is small. Subtree editing requires an explicit complete response, and capture-phase submission guards prevent truncated previews from being sent as edits. Invalid retained subtree drafts remain editable. Fold controls use CodeMirror rather than an eagerly populated hidden line DOM. JSONL/CSV native subtree editors use their JSON representation language.

Find clears abort actual fetch requests. Whole-document mutations use session-scoped asynchronous jobs, stage polling, and explicit server cancellation. The enqueue response is retained long enough to obtain a cancellation ID. If commit wins a cancellation race, the client obtains the committed metadata. Other expensive reads expose progress and request cancellation. Inspector forms intercept htmx before its XHR starts and submit tracked edit jobs. Conversion previews indicate truncation, and converted downloads explicitly fetch revision source instead of relying on a removed metadata `source` field. Comparison results are cached by revision and baseline; explicit Refresh bypasses the cache.

## Rebuild

Validated on Node v24.22.1:

```sh
npm ci --offline --ignore-scripts
npm run build:web
npm test
```

The lockfile pins dependency versions. A first dependency installation requires registry access; subsequent offline installs require the packages in npm's cache. `build:web` bundles production code into `internal/server/web/app.bundle.js` and writes bundled dependency licenses to `internal/server/web/vendor/editor.LICENSE`. The page uses only local scripts; CodeMirror has no runtime CDN imports.

Two consecutive builds produced the same bundle SHA-256:

```text
5e3ed1a5aedf3ff439b17fddcfddda30a3bb12064c2c2413dd9170cc7448907f
```

## Verification

- Fresh offline dependency install: 55 packages installed; audit reported zero vulnerabilities.
- Full `npm test`: 102 passed, zero failed/cancelled/skipped.
- Syntax checks for `app.js`, `inspector-editor.js`, and `request.js`: passed.
- `git diff --check` for frontend/package/build paths: passed.
- jsdom runs the real CodeMirror adapter. Coverage includes a 100,000-line document with fewer than 200 rendered `.cm-line` elements, composed Unicode/UTF-16 patches, CRLF normalization, labelled keyboard Tab editing, read-only state, selection without focus theft/feedback, and draft snapshots.
- App-level jsdom coverage verifies metadata startup request counts, patch Apply without whole-source serialization or another source fetch, tree-to-source node lookup, source-to-tree locate requests, and lazy inspector loading.
- Inspector coverage verifies bounded preview mount, no hidden representation mount, blocked preview submission, explicit full-text editing, and aborted responses that cannot mount later.
- Request coverage verifies job polling, stages, cancellation while polling or enqueueing, and the commit-versus-cancel race. Existing tree, context menu, conversion, comparison, Find, file picker, and validation tests remain green.

This report contains Node/jsdom evidence, not native-browser proof. Native IME, real scrolling/paint behavior, screen-reader behavior, and the 10/50/100 MiB p95 and memory targets remain subject to the integrated browser/performance qualification. The adapter uses CodeMirror's standard composition and keyboard handling, but synthetic jsdom events do not establish native IME correctness. No 100 MiB acceptance or limit increase is claimed here.

`tree.js` and `tree.test.js` user edits were preserved; this frontend work did not modify them. Go/server verification is recorded by the coordinating workstream.

## Review follow-ups

Committed metadata now begins a canonical refresh phase: mutation cancellation is detached, Cancel is disabled, and source/tree reads finish before the applied view is replaced. Failures in these reads explicitly report the committed revision and view refresh failure. Inspector writes now use tracked jobs; cancellation waits for a server terminal result rather than treating an unchanged metadata snapshot as rollback proof. A completed edit installs its metadata, and failed or cancelled edits retain the mounted form and draft with an inline alert.

Read-only JSON/YAML representations now expose an explicit full-text loading action for complete copying. They remain read-only after loading. Truncation is tracked separately from status text, so a failed full-text request cannot make the earlier truncated preview copyable.

The four observed regressions were reproduced as failing tests before fixes. Added coverage also delays committed import source reads while Cancel is pressed and checks committed refresh error messages. The full Node suite passes 102 tests, including bounded Find next/previous-page behavior. Two final bundle builds are identical. Native browser and 100 MiB qualification limits above still apply.

## Geometry-preserving source Apply refresh

An opt-in fast install now requires an applied source draft, the immediately following revision, stable IDs, a changed-node ID list, and the same root ID. Only already-loaded changed nodes are requested, ordered root-to-leaf at their previous page offsets. Other cached tree records, expansion state, selection, canvas view, and source locations remain available; the draft becomes the clean applied snapshot. Nested depth-2 summaries cannot overwrite independently loaded child pages. Other responses retain the normal full refresh path. This relies on the backend guarantee that normalized UTF-16 source geometry is unchanged.

The new app regression failed before implementation and now passes. It observes exactly root offset 0 and changed ancestor a offset 20 requests, while unrelated b offset 20, the selected b20 node, source caret, lookup cache, record map, expansion set, and canvas pan/zoom remain retained. No source or node-location fetch occurs. The complete Node suite now passes 102 tests. The final bundle was built twice with identical output. Parent Original/source synchronization behavior remains unchanged.

## Tracked Inspector writes and terminal cancellation

Inspector htmx beforeRequest events are prevented before a legacy XHR can start. Collected parameters, or FormData with the actual submitter, become an /api/edit job request. Structured string, number, boolean, and null controls validate the chosen JSON kind; number lexemes are sent unchanged, and checkbox flags are converted to booleans. Delete actions confirm the submitted operation. Full subtree text comes from the mounted complete editor. Invalid local kinds and server validation errors retain that editor, the form, and the draft with an inline alert.

Cancellation waits for complete, failed, or cancelled job status independently of the aborted UI signal. A cancelling acknowledgement alone does not release the operation. If commit wins the race, its metadata is installed through the canonical refresh phase. The previous speculative metadata-read recovery and untracked Inspector XHR path have been removed.

Three new regressions were observed failing before these fixes and now pass. Coverage verifies prevention of the legacy XHR, invalid subtree retention, full-editor submission, a committed edit versus Cancel, exact structured number text, type validation, destructive confirmation, and a cancelling-to-cancelled terminal transition. These application tests exercise the htmx event boundary with jsdom and mocked network responses; they do not replace native-browser or server integration evidence.
