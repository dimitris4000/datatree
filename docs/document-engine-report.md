# Document engine implementation report

This records the initial scanner/shared-tree stage. Later compact parsing and local-context work supersedes its remaining-work list; see [compact adapter report](compact-adapter-report.md).

## Implemented

- JSON/JSONL use a strict direct scanner that builds ordered YAML nodes and exact byte spans without a second YAML parse. Number lexemes remain exact, decoded duplicate keys are rejected, and escaped strings retain `encoding/json` Unicode/surrogate semantics. Locations use source-rune columns. JSON avoids per-line Unicode offset arrays, repeated flow scans, mapping-key marshaling, and alias lookup maps.
- YAML flow collection spans are indexed before descending into children and nested cached spans are skipped. Repeated YAML/CSV scalar mapping keys reuse the original `yaml.Marshal` fingerprint, keyed by all serialized scalar properties, with a 4096-entry per-parse cache. Complex keys retain the original path.
- `SourceRange`, `Locate`, and `Ancestors` use a lazy immutable source index with `sync.Once`, atomic publication, ID lookup and an interval index. Coordinates remain normalized UTF-16, including CRLF normalization. Cursor containment uses half-open terminal boundaries; complete selections can include a node's end. `SourceRanges` remains a caller-owned compatibility copy.
- `FindContext` checks cancellation during search and query work, supports bounded `Limit`/`Cursor` pages and returns `NextCursor`; the default remains 200 with `Truncated`. Query grammar/work limits remain intact.
- `ParseWithLimitsContext` checks cancellation during JSON scanning, decoder reads, raw validation and visible-tree construction. `EditContext` propagates context into fragment parsing and patched-result validation.
- `RepresentationContext` returns a bounded UTF-8 prefix and exact complete byte count. JSON formatting streams to the prefix collector. Native JSON normalizes CRLF; native YAML preserves standalone subtree dedenting. YAML output streams through an encoder. Bounded CSV previews can count discarded JSON output beyond the input byte limit; explicit full conversion retains the conversion byte limit.
- JSON scalar/subtree replacements patch source before validated reparse, preserving untouched formatting and avoiding a complete raw-tree clone/re-encode.
- JSON/JSONL scalar patches with unchanged byte, source-rune and supplementary-rune counts path-copy the target and raw/node ancestors, share unchanged nodes, preserve IDs and reuse an already-built immutable source index. Leading/trailing whitespace and raw newlines are excluded. `TryScalarPatchContext` exposes this conservative path; `StableChanges` returns a caller-owned target/ancestor refresh list. Structural edits and diff reverts canonicalize shared parent pointers before using the full-clone path.

## Verification

- New APIs and regressions were observed failing before implementation: indexed lookup, find pagination/cancellation, source-preserving JSON replacement, query/parser cancellation, bounded representations, edit cancellation, incremental sharing, UTF-16 geometry, bounded CSV conversion, and stable refresh metadata.
- Latest `go test ./internal/document`: PASS, 1.759s after the revision-retention fix.
- `go test -race ./internal/document`: PASS, 20.598s, before the final scalar-fingerprint cache and stable-change metadata additions. The coordinator owns the final repository-wide race gate.
- Strict JSON grammar fuzzing: PASS, 38,902 executions in 6.352s. Seeds cover malformed numbers, delimiters, escaped strings/surrogates and decoded duplicate keys.
- Existing source-map, UTF-16/CRLF, exact-number, YAML aliases/comments/streams, CSV quoted-newline, query, schema and diff tests remain in the passing document suite. A retention regression was observed failing on the previous full parent pointers, then passing after thin ancestor identities; it alternates 40 edits and verifies schema/diff marks, diff revert and structural delete.
- Coordinator measurements are recorded separately; this report does not assert browser or 100 MiB acceptance.

## Remaining limits

- This is not a complete compact persistent document-engine rewrite. `Document.Source`, ordered raw nodes, visible nodes and the ID map remain materialized. Ordinary source-changing patches still reparse the complete document.
- Even the same-span scalar fast path copies the source bytes and ID map. Its AST sharing is conservative; arbitrary length-changing edits, JSONL record-isolated reparsing, CSV row-isolated reparsing and general smallest-subtree YAML reparsing are not implemented here.
- Copied fast-path nodes use lightweight ancestor identity chains without raw nodes or children, preventing predecessor revision-graph retention during alternating edits. Unchanged original nodes can retain their initial parse graph; the chain does not grow per revision. Mutation paths requiring canonical raw/parent identity explicitly reparse first.
- The source index is lazy but its initial construction remains proportional to document size. Legacy `SourceRanges` copies all ranges. Later find pages currently traverse preceding nodes; query pages recompute the bounded query result.
- Bounded representations avoid storing complete serialized output, but JSON escaping can allocate one scalar's escaped form, YAML style normalization clones the raw subtree, and the YAML encoder has internal buffering. Computing exact `totalBytes` still visits the complete representation.
- Cancellation is cooperative. Existing clone/serialization fallbacks and work inside third-party YAML decoding/encoding can run until their next explicit check, read/write or parse boundary.
- Session history/text-store integration, server concurrency, frontend virtualization and acceptance benchmarks belong to the coordinator/other workers. Existing default limits remain unchanged.
