# Compact document stage

The approved plan still requires compact records and general local parsing. Implement this stage without weakening existing format semantics or claiming native-browser qualification. Existing optimized legacy paths remain the compatibility fallback. Default limits stay unchanged until qualification.

## Ownership

- Coordinator: `internal/textstore` anchors/positions, integration/performance qualification and documentation.
- Compact worker: new `internal/compact` only, independent of document/parser/server packages.
- Integration worker: `internal/document` and `internal/server` adapter/local parsing and tests. Preserve current legacy functionality and workstream changes.
- Editor worker: tracked Inspector jobs first; later geometry-aware refresh if necessary.

## Text store interfaces

`type Anchor uint64` is an immutable source-piece identity and offset. `Text.AnchorAt(byteOffset int) (Anchor,error)` resolves a UTF-8 boundary. `Text.AnchorIndex() *AnchorIndex` builds an immutable origin-piece lookup in O(number of pieces), and `(*AnchorIndex).ByteOffset(Anchor) (int,error)` resolves an anchor in this text version. Removed interior anchors return an error. Endpoints touching surviving fragments remain resolvable. Empty text uses anchor zero.

`Text.Position(byteOffset int) (Position,error)` returns `Position{UTF16,Line,Column int}` using normalized CRLF/CR coordinates and rune columns, without flattening. Existing ByteOffset resolves normalized UTF-16 to bytes. Split leaves preserve origin identity. Replace operations remain immutable and balanced.

## Compact package interfaces

Use `uint32` IDs (zero invalid), existing document kind strings publicly and compact enums internally. Definitions:

```go
type Input struct {
 ID, Parent, Target uint32
 Children []uint32
 Kind, Label, Value string
 Start, KeyStart, End int // byte offsets in supplied source; KeyStart<0 uses Start
 Pathable bool
 NativeIndent int
 Weight int // this value plus its own mapping key's raw nodes, minimum 1
 HasMetadata bool // conservative YAML metadata/reference fallback flag
}
type Record struct {
 ID, Parent, Target uint32
 Kind, Label, Value string
 Start, KeyStart, End int
 ChildCount, Index, NativeIndent, Weight int
 Pathable, HasMetadata bool
}
type Change struct {
 Affected []uint32 // target, nearest parent, ... root
 RemovedRoots []uint32 // old child roots invalidated inside target
 StableGeometry bool
}
func New(ctx context.Context, text textstore.Text, nodes []Input, root uint32, maxNodes,maxDepth int) (*Tree,error)
func (*Tree) Text() textstore.Text
func (*Tree) Root() uint32
func (*Tree) Count() int
func (*Tree) Weight() int
func (*Tree) Record(id uint32) (Record,bool)
func (*Tree) Children(id uint32,offset,limit int) []uint32
func (*Tree) Path(id uint32) string
func (*Tree) LocateByte(start,end int) (uint32,bool)
func (*Tree) Preorder(ctx context.Context, visit func(Record) bool) error
func (*Tree) Replace(ctx context.Context,target uint32,replacement []byte,fragment []Input,fragmentRoot uint32) (*Tree,Change,error)
```

`Replace` replaces exactly target's value-byte span, keeping target ID, parent, key label/key span and mapping-key weight. Fragment offsets are relative to replacement bytes; its root label/parent are ignored. Descendant IDs may change; unrelated IDs remain stable. Source anchors outside the replaced span move automatically. Refresh ancestor boundary anchors that touch a replaced endpoint. Enforce global raw-node weight, depth and cancellation before returning; no original mutation on failure. Reuse deleted ID slots or otherwise reclaim obsolete record data so repeated subtree replacements do not grow retained storage unboundedly. Record storage uses persistent chunks: cloning a version must not clone every row or retain predecessor Trees. Child vectors are immutable, returned pages are copied. Duplicate/malformed fragments are parser responsibility; structural invariants are checked by Tree.

## Document adapter

Start automatic compaction above 1 MiB; add explicit `ParseCompactWithLimitsContext` for small targeted tests. Compact imports can initially build from the validated legacy parser, then discard raw/application graphs. This bounds retained memory while direct compact parsing can improve first-load peak later. `Document.Source` remains a current immutable contiguous source for compatibility (one source copy per applied version is acceptable); compact records and text/history share unchanged data.

Introduce `Document.NodeCount()`, `LookupNode(id string) (*Node,bool)`, `ChildPage(id string,offset,limit int) ([]*Node,int)` and migrate server core reads away from `.Nodes` and full `.Root.Children`. Root remains a shallow summary. Native JSON/YAML/CSV/JSONL source ranges, lookup, Find and direct path queries should use compact records; source navigation must not materialize the full legacy AST. Location/path strings are generated on demand. Existing raw-node feature implementations may materialize a temporary legacy document for explicit full conversion, complex diff/schema work and unsafe YAML edits. Materialization remaps IDs by parallel tree traversal to retain the compact stable IDs; never cache the full materialization indefinitely.

Local source patches choose the smallest safe context. JSON parses the enclosing value/container; JSONL parses one affected record; CSV scalar editing parses/quotes only its field (row/context fallback when needed); YAML reparses a standalone safe scalar/context and falls back for anchors, aliases, tags, metadata-sensitive or multiline context changes. Validate fragment semantics and global limits before compact replacement. General structural/whole-source operations may explicitly use the full fallback; ordinary variable-length scalar edits must not reparse the full file. Session history stores text versions and should restore compact documents without permanently retaining legacy graphs.

The current frontend `stableIDs` contract also promises unchanged source geometry. Do not emit that promise for variable-length changes until a separate `geometryStable` field and source-location-cache invalidation are integrated. Existing safe same-span refresh must remain correct. Compact structural updates can use the ordinary full UI refresh until targeted invalidation is safely supported.

## Tests and acceptance

Test compact versions against legacy semantics for all formats, byte/UTF-16/CRLF bounds, exact numeric text, metadata fallback, aliases, duplicate keys, invalid patches, key edits, page/index/path lookup, multiple local edits, undo/redo, concurrency and bounded retention. Verify unrelated rows/source anchors are shared, not merely identical. Rerun all functional/race/JS/build checks, sample benchmarks and 10/50/100 MiB workflows. Record peak versus retained allocation honestly. Browser qualification remains separate until an actual browser test is available.

## Postorder import builder

`compact.NewBuilder(ctx, text, maxNodes, maxDepth)` accepts one `Input` at a time through `Add`, with children added before their parent. `Finish(root)` validates root reachability and alias targets, publishes one immutable Tree and closes the builder. Duplicate/repeated/missing children, parent/span ordering, depth/weight, cancellation and invalid roots reject publication; errors permanently invalidate that builder. This avoids a document-sized intermediate Input slice. Existing New/Replace APIs remain for independent callers and fragment replacement. CSV streams directly to this API; JSON/JSONL/YAML use it while consuming their validated raw parser tree.
