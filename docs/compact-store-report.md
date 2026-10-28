# Compact record store report

## Implemented contract

`internal/compact` is independent of document, parser and server packages. It exposes `Input`, `Record`, `Change`, `New`, `Text`, `Root`, `Count`, `Weight`, `Record`, `Children`, `Path`, `LocateByte`, `Preorder` and `Replace` from the compact-engine contract. `Input.OwnDepth` additionally records raw mapping-key depth that is invisible in the displayed value tree.

Records use compact kind enums, numeric parent/target/child identities and source-piece anchors. Public records resolve current byte offsets through one immutable per-version text anchor index. Paths and source positions are generated on demand; mapping-key ranges participate in navigation. Cursor containment is half-open at nonempty node endpoints, while nonempty selections may include an endpoint. Pages and change lists are caller-owned copies.

Storage is persistent in 256-record chunks. A replacement copies the chunk directory and touched chunks, shares unrelated chunks and immutable child vectors, and retains no predecessor Tree or parser/application object. Deleted rows are zeroed, empty chunks are removed, and deleted IDs are reused. Free-slot storage contains numeric IDs, not obsolete record data. Source pieces are shared through textstore.

Replacement preserves the destination value ID, parent/index, mapping key label/span and raw key weight/depth. Fragment root mapping-key properties are discarded; its value contributes one raw node. Descendant IDs are remapped and unrelated IDs remain stable. Ancestor endpoints touching the edited span are refreshed; other ranges move through their anchors. `Affected` lists target through root; `RemovedRoots` lists the previous immediate child roots. StableGeometry conservatively requires unchanged value byte endpoints, normalized UTF-16 endpoints and rune-column span on one line.

Construction checks structural reachability, unique IDs, parent/child consistency, ordered nonoverlapping nested source spans, valid kinds, alias target identities, raw-node weight and visible/raw-key depth. Replacement validates the fragment, global resulting weight/depth, references to removed records, source boundaries, UTF-8 and cancellation before returning. Failures preserve the original version. Fragment/source syntax and format semantics remain the parser's responsibility.

Ancestor counts and weights propagate through local deltas. Heights remain unchanged for same-height scalar changes. A decreased maximum child height may scan that ancestor's child vector to find the new maximum.

## Verification

RED-to-GREEN evidence includes initially missing compact APIs, discarded fragment key weight incorrectly exceeding limits, raw-key depth support, and cancellation after a preorder visitor stops.

Tests cover immutable source/records/pages, stable unrelated IDs, exact anchor movement, UTF-16/CRLF positions, escaped paths, source-key navigation, subtree deletion/insertion, mapping key weights and raw depth, global replacement limits, malformed hierarchy/alias targets, cancellation, empty-field insertion, repeated variable-length edits, stable-geometry boundaries, and concurrent independent versions.

Sharing tests compare unchanged chunk pointers, ancestor child-vector backing storage and source-piece anchor identities. Forty alternating large-subtree expansions/deletions check that empty chunks disappear, ID capacity remains bounded and deleted slots retain no label/value/children. Another test performs 100 subtree replacement cycles.

Fresh commands:

- `GOCACHE=/tmp/datatree-compact-gocache go test ./internal/compact ./internal/textstore`: PASS, compact 0.247s; textstore 0.479s.
- `GOCACHE=/tmp/datatree-compact-gocache go test -race ./internal/compact`: PASS, 1.347s.
- `GOCACHE=/tmp/datatree-compact-gocache go vet ./internal/compact`: PASS.
- `git diff --check`: PASS.

Core-only measurement on the current machine, 100,001 records, ten replacements:

`BenchmarkScalarReplace100KRecords`: 246,626 ns/op; 161,632 B/op; 70 allocations/op.

This measures persistent Tree replacement only. It excludes construction, parser/adapter work, contiguous source materialization, server responses and browser interaction. It does not establish the overall 100 MiB acceptance targets.

## Explicit limits and remaining integration

- New construction remains proportional to all supplied records. Initial legacy parsing and direct compact parsing are outside this package.
- Each version copies the chunk directory, proportional to chunk count, and builds a text anchor index proportional to source-piece count. It does not copy every record.
- Replacing a large subtree visits its removed descendants and incoming fragment. A temporary fragment Tree is built before insertion. Peak allocation is therefore larger than the final retained changed chunks.
- ID free lists can retain numeric capacity proportional to peak live slots. Obsolete record strings and child vectors are cleared; no predecessor Tree is retained. Text-piece backing-storage reclamation belongs to textstore.
- Preorder traversal uses a stack of pending child IDs; a very wide collection can require temporary storage proportional to its width. Paths are generated by walking parent identities.
- Format semantics, exact scalar validation, YAML metadata/reference safety and duplicate-key checks must be enforced by the adapter/parser. The conservative HasMetadata flag is preserved; compact replacement does not itself parse syntax.
- When fragment root offsets exclude leading/trailing replacement trivia, the value uses those exact offsets while touched ancestor endpoints continue covering the enclosing inserted byte extent. Normalized standalone scalar fragments avoid this boundary distinction.
- Contiguous `Document.Source`, temporary legacy materialization, source/history integration, automatic compaction thresholds and frontend refresh remain adapter/coordinator responsibilities. No native-browser or full application performance qualification is claimed here.
