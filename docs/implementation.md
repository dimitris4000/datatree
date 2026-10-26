# Data Tree implementation record

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
