# Data Tree implementation record

## Saving converted copies

Conversion previews include the suggested basename/target extension and, for locally opened files (including CLI startup files), their resolved directory. `/api/convert` accepts an optional `saveFilename` constrained to a filename with the target extension in that directory. `Session.ConvertWithSave` validates converted output before invoking exclusive file creation, then commits history only after the write, sync and close succeed. Existing files and symlinks are never overwritten; failed partial writes are removed. Errors retain the working document and editable preview. The original file identity stays attached, and Undo affects memory rather than deleting saved copies.

The conversion dialog offers an unchecked save option and editable suggested filename. Uploads have no trusted local directory and instead offer a named browser download. Controller tests cover local save, upload download, conversion without save, stale previews, invalid filenames, retry after errors and concurrent submission. HTTP tests verify real saved bytes, directory/name suggestions, unchanged originals, collision rejection and history behavior.

## Startup file

The CLI accepts one optional positional file after flags. `server.NewWithFile` reuses local-file validation before `net.Listen`, so invalid startup input cannot leave a running server. The validated document is an immutable startup snapshot; new browser sessions receive independent original bytes, revision/history and file provenance. Later file changes do not alter that snapshot, and editing remains in memory. Startup tests cover supported extensions, session isolation, malformed content, missing files and argument errors.

## Schema validation

CSV uses the same schema projection and diagnostic targeting as JSON and YAML: the instance is an array of row objects with string-valued cells. Upload and local-open workflow regressions cover CSV, YAML and the `.yml` alias through querying, inspection, editing, search, diff/revert, history, schema validation, conversion and download.

Approved design and execution checklist: `superpowers/specs/2026-10-27-schema-validation-design.md` and `superpowers/plans/2026-10-27-schema-validation.md`.

Schema validation uses vendored jsonschema/v6 v6.0.3 and x/text v0.14.0. Compilation permits built-in standard dialect metadata and embedded schema resources, with an external loader that rejects all filesystem/network fetches. Exact-number validation projects the ordered AST without lossy export. Separate provenance maps route aliases, inherited merges, stream documents and missing/unexpected property errors to current visible nodes.

POST `/api/schema`, `/api/schema-clear` and `/api/validate` enforce origin/session/revision guards. Attachment/validation work snapshots immutable inputs outside the global lock and rechecks live session identity, document revision and schema generation before publishing. Attachment is separate from history and resets only after successful import/open. State responses restore metadata after reload; validation results remain frontend state and require explicit Validate.

The focused frontend controller prevents late responses from restoring stale badges/navigation. The panel, keyboard tree list and Inspector render schema content as text; navigation reuses paginated source-map revelation and existing draft confirmation. Validation and diff badges clear independently.

Review regressions cover enormous zero exponents (which can panic the upstream rational-number evaluator), malformed built-in YAML scalar tags, custom merge tags, empty property-name keys and reference evaluation cycles. A conservative preflight budget prevents multiplicative schema/instance error expansion before invoking the library. Only 200 diagnostic details are retained; marks cover all evaluated errors, and displayed paths/messages/value previews are bounded. This is a work estimate, not a wall-clock execution deadline.

Browser inventory in this session is empty. Live layout, keyboard and synchronized-source acceptance remain unverified; automated coverage is recorded in the feature plan.

Spec: the accepted Go Data Tree Editor plan in the project conversation.

1. Parsing, source spans, limits and conversion.
2. Atomic edits, revision checks and history.
3. Loopback HTTP service and htmx/p5.js UI.
4. Integration, browser/layout verification and documentation.

Ruling: use go.yaml.in/yaml/v3 v3.0.4, available in the local module cache, rather than goccy/go-yaml, which cannot be downloaded in this environment. Source preservation uses raw-byte patches and independent source spans, never whole-file YAML serialization for local edits.
Ruling: this is an empty temporary workspace, without a Git repository; work in place and do not create commits or a second worktree.
Ruling: enforce a 64 MiB history budget and a maximum of 30 history revisions, with a 30-minute idle session expiry, to bound local memory usage.

Ruling: p5.js is unavailable in all local caches and shell DNS/network access is blocked. Ship an actual native Canvas 2D fallback and a pinned p5.js vendoring script; p5 mode uses the same renderer through p5's canvas. The missing library is documented, without substituting a fake p5 implementation.
Ruling: HTTP integration tests use httptest.ResponseRecorder; loopback socket creation and headless browser startup are denied by this sandbox, so visual/browser acceptance remains unverified.

Independent review found block/plain YAML scalar spans consuming compact-mapping siblings, explicit scalar indentation changing content, invalid subtree fragments escaping into siblings, and exponential merge expansion. Regression tests reproduced each failure before fixes. Spans now use semantic key indentation, native snippets dedent, unchanged applies preserve bytes, fragments validate before patching (with bounded anchor stubs for external references), and merge resolution is memoized with a work budget.

## JSON-to-YAML formatting

Conversion and non-YAML inspector representations serialize a cloned node graph with presentation styles cleared. Scalar tags and values remain intact, preserving ambiguous strings, numeric precision and key order. The YAML encoder uses two-space indentation and retains explicit numeric tags when required for very large integers. Ordinary YAML edits keep their existing style-preserving serialization path.

Regression coverage includes nested objects and arrays, empty collections, multiline and ambiguous strings, large numbers, immutable preview/inspection, preview-to-confirmation equality, downloads and undo.

## Node context menu

Right-click uses the canvas hit test and viewport transform to target card headers or object scalar fields. An HTML menu shares the existing edit endpoint and draft/busy guards. Object duplicates and renames prompt for a key; array items duplicate directly; deletion confirms. Root actions, array renaming and CSV cell shape changes are disabled. Existing inspector controls remain available.

The menu supports arrow keys, Home/End, Escape and Shift+F10 from the node list. It stays within the viewport and closes on outside pointer input, pan/zoom, scrolling, resize, focus loss or document changes. JavaScript regression tests cover hit targeting, action eligibility, cancellation, request payloads, positioning, focus and keyboard dismissal.


## Source editor selection

The native textarea has an unwrapped, viewport-sized line-number gutter, a line/column indicator, and a text mirror that keeps source highlights visible after focus moves to the tree. The gutter renders only visible lines. The editor retains native keyboard, selection, clipboard and IME behavior.

`GET /api/source-map?revision=N` returns a bounded, flat index of node IDs, parent IDs, child positions and source ranges, without scalar values or the complete expanded tree. Ranges include mapping keys and use textarea coordinates: UTF-16 code units with CRLF normalized to LF. JSON/JSONL/YAML use parser spans; CSV positions come from the standard CSV reader, with quoted and multiline fields scanned to their closing quotes. The synthetic root covers the whole source.

Tree selection highlights the corresponding source range without stealing focus. Editor caret and selection changes are debounced and choose the smallest enclosing range; selections spanning siblings choose their common container. Ancestor pages are loaded before revealing the node. Programmatic highlights suppress feedback, pending reveals are invalidated by revisions, edits, tabs and tree selections, and existing inspector draft confirmation remains in effect. Source sync runs only when displayed text matches the applied document, including the Original tab only when its text is identical. Existing canvas card limits remain in effect.

Verification covers Unicode, CRLF, YAML keys, JSONL roots, CSV multiline fields, range hierarchy, revision and session isolation, off-page reveal paths, viewport gutter numbering, source highlights and selection feedback suppression. Live browser verification for this change is unavailable because no browser is connected to the computer-use tool.


## Compact toolbar and Git context

File actions, document identity and editing tools share one toolbar. Branding and the local-workspace label are removed; the existing live status region now occupies the footer center. One Open file button launches a dialog containing the file picker and an optional local-path form for Git context. No native file input or separate path action appears in the toolbar. A global hidden rule prevents input styles from overriding hidden elements.

Browser uploads do not expose trustworthy filesystem paths. `POST /api/open` explicitly opens a server-local path, checks the revision, supported extension, regular-file type and 10 MiB size limit, and imports only after successful parsing. Session-level filename and path survive document edits and history; a successful browser import clears the path. Failed opens retain the previous document and provenance. Existing loopback and same-origin checks apply.

Git context uses bounded, read-only Git commands without a shell and with literal pathspecs. Only tracked files with a valid HEAD receive a branch and commit; detached HEAD has an empty branch. Git environment overrides inherited by the server are excluded. State responses re-read checkout metadata, while document edits remain in memory. No Git executable or no repository means no badge. Tests use isolated temporary repositories for branches, detached HEAD, untracked files, session isolation and rejected requests. Browser visual verification is unavailable because the computer-use inventory has no connected browsers.

## Search and bounded path queries

`Document.Find` searches the complete ordered AST, independent of rendered pages. Literal searches support keys, scalar values and optional case sensitivity. Complex YAML keys are searched through their actual scalar parts; aliases remain reference leaves. Result collection deduplicates by node and preserves document order, with a 200-result cap and a distinct truncation flag.

The dependency-free query parser accepts properties, indexes, wildcards, recursive named properties and single typed comparison filters. Queries never execute code or expand aliases. Traversal has a one-million-unit work budget and a 256-step limit; request text and numeric operands are limited to 4,096 bytes. Numeric comparisons use decimal digits plus a big-integer magnitude rather than floating point or exponent expansion, retaining precise comparisons of large integers and very large exponents. YAML integer bases and decimal presentation forms are normalized. Streams return a search suggestion; nodes without JSON-compatible paths are excluded.

`GET /api/find` requires a document revision and uses the existing session, loopback and cache protections. `find.js` owns result state, ignores superseded responses, and renders all document content as text. Mode/scope/text changes and revision installation invalidate results and pending reveals. Navigation uses the existing source-map hierarchy, draft guards and 20-child pages. Tree layout optionally prioritizes the result’s ancestor chain before allocating its 1,000-card budget, retaining sibling placement order and ensuring deep matches remain visible.

Verification includes ordered/scope-aware searches, complex keys and aliases, query syntax rejection, Unicode and escaped keys, exact typed comparisons, query work/result limits, HTTP revision/session isolation, safe panel rendering, late responses, cancellation, off-page results and deep result visibility under the rendering cap. The full Go tests, race tests, vet and 42 JavaScript tests passed during this implementation. Live browser verification is unavailable: the computer-use inventory reports no connected apps or browsers.

## Structural diff

`document.Diff` compares original/baseline and working ASTs by typed mapping keys and sequence indexes. It returns at most 200 bounded previews, complete added/removed/changed counts, and working-node marks. Removed changes target the matched current parent. Added/removed collections are summarized once; added subtrees receive marks on all their current nodes. Ancestors carry contains-changes flags. The comparator never mutates either input.

Canonical identities normalize standard scalar values (including precise decimal numbers and YAML integer bases), retain custom tags and typed key distinctions, and sort mapping-entry fingerprints. Aliases use structural target locations without expanding target content. Complex key identities use scoped internal reference locations so cyclic key aliases do not leak provisional addresses into external value or key references. Regression tests cover the external-alias and alias-key cases identified during review, plus key reordering and anchor renames. Fingerprinting, comparison and ancestor marking share a 64 MiB-unit work budget; numeric values are limited to 4,096 bytes. Errors return no partial result.

`Session.OriginalFormat` retains the initial parsed format across conversions/history. The read-only revision-scoped diff endpoint reparses original bytes with that format, or uses one parsed session comparison file. Comparison-file replacement parses and computes the diff before atomically committing the baseline, without creating a document revision/history entry. Working import/open resets it only after success. The existing session, origin, loopback and input limits apply.

`diff.js` owns panel state and serializes file uploads, ignores obsolete revision responses, preserves baseline/results on failed replacement and clears highlights on close or document replacement. `app.js` shares the prior guarded source-map navigation between Find and Changes; open Changes refreshes after installing an accepted revision. Canvas cards/rows and the keyboard node list include explicit status labels and colors. Previous/Next use only returned working targets, including surviving parents for removals. All previews are rendered as text.

The browser inventory has no connected apps or browsers, so live visual acceptance remains unverified. Automated verification and review results are recorded in the structural-diff implementation plan.


## Selectable diff previews and per-change revert

Diff entries use ordinary focusable preformatted Before/After text, with heading navigation and Revert buttons as sibling controls. Text selection does not navigate, and previews remain bounded and rendered as text. Mutation controls lock during revert; existing source/subtree draft guards apply.

`POST /api/diff-revert` checks the current revision and a SHA-256 baseline source/format token before recomputing the selected diff index. Server-only node references provide full inverse values; previews are never edit payloads. Added entries are removed, removed entries are inserted relative to surviving baseline siblings (sequences retain indexes), and changed nodes are grafted from the baseline. A private AST clone is validated and serialized before one session history commit. Baseline data remains unchanged.

YAML grafting restores typed/complex keys and tags, resolves internal/cyclic aliases, maps external references by structural location, and renames colliding anchors. Dangling references and unrepresentable format changes are rejected atomically. Local patches preserve bytes outside the affected value/collection; external anchor changes can widen the patch. Missing earlier array positions must be restored before later removed positions.

Verification passed: full Go tests, race tests, vet, all 53 JavaScript tests, syntax checks and diff whitespace checks. Regressions cover full values beyond preview length, nested/root changes, JSONL/CSV, YAML keys/CRLF/source preservation, reference retargeting and collisions, atomic rejection, history, stale baseline replacement, safe selectable preview DOM and mutation locking. Read-only review reported no material issues. Live browser text-selection/layout verification remains unavailable without a connected browser.
