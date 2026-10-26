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
