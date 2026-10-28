# Compact document adapter

## Implemented

`ParseWithLimitsContext` automatically switches retained storage above 1 MiB. `ParseCompactWithLimitsContext` makes the same mode available to small fixtures. Imports validate through the existing raw parser, then emit compact records with source anchors, raw-node weights and mapping-key depth. JSON/JSONL/YAML avoid constructing a full application graph; CSV temporarily constructs and discards it. Raw parser graphs are not retained. Root is a shallow summary. `Document.Source` remains one immutable contiguous copy for compatibility.

`NodeCount`, `LookupNode` and `ChildPage` serve shallow summaries and bounded child pages. Server state, tree pages, inspector and representation lookup use them. Source ranges, source lookup, ancestors, normal search and direct key/index queries operate on compact records; they do not rebuild a legacy AST. Paths and line/column locations are produced when requested. Search pagination preserves preorder cursor semantics and direct queries retain the existing query-work budget.

Native JSON/YAML representations stream bounded previews from source; YAML retains standalone subtree dedentation. JSON representations of simple JSON/JSONL/CSV/YAML stream from compact records with exact numeric text. Metadata-sensitive YAML, non-string/complex keys and YAML streams can fall back to temporary legacy representation work.

Variable-length scalar source patches parse only the containing scalar context. JSON replacement actions also parse and splice a whole selected value/container locally. JSON/JSONL key source edits grow to the containing JSON value; add/rename/delete/clone parse and edit only the selected collection or target parent. A broad root container can still be the required context. JSONL local changes preserve one-record constraints. CSV actions quote one string field and local source patches validate one field, including quoted newlines and empty cells. Safe single-line YAML scalars are parsed in isolation; anchors, aliases, comments/tags, flow context, multiline values and metadata-sensitive edits use the full validated fallback. Compact Replace checks total raw-node weight, depth, cancellation and source bounds before returning. Duplicate-key/invalid fragment failures leave the old document untouched. Source edits that expand CSV record or scalar syntax context fall back to complete validation rather than rejecting a potentially valid document.

Unchanged records and source pieces use the persistent compact/text stores. Target and unrelated IDs survive local changes; removed descendants may receive recycled IDs. Same-span scalar changes retain the existing stableIDs/unchanged-geometry promise; variable-length or structural changes deliberately use the ordinary full UI refresh.

Session snapshots retain compact Trees and persistent text versions, rather than each Document's flat Source or any raw AST. Undo/redo reconstruct one current flat Source and reuse the compact version without reparsing it. Local action commits use the compact text directly and avoid scanning the entire source to discover a diff.

Raw-node features (full conversion, diff/revert, schema evaluation, structural fallback edits and advanced queries) explicitly materialize a temporary validated legacy document. Parallel tree traversal remaps fresh parser IDs onto the compact revision's stable IDs. Materializations are never cached in the compact document.

## Test-first evidence

The initial adapter test failed to compile because explicit compact parsing and accessors did not exist. The native YAML parity test subsequently found and corrected stream/non-string-key representation fallback. Added coverage includes all four formats, normalized ranges, exact numbers, bounded pages, Unicode/CRLF, variable-length scalar changes, repeated edits, empty/quoted/multiline CSV cells (including quoted empty sole-column actions and blank-record-removing source patches), independent IDs, schema ID remapping after structural replacement, YAML metadata/aliases/complex keys/streams, duplicate keys, global limits, cancellation, history, and core HTTP reads/patches.

At the time recorded:

- `GOCACHE=/tmp/datatree-go-cache go test ./internal/document ./internal/server -count=1`: passed.
- Targeted compact adapter suite: passed after the latest navigation/query/serialization changes.
- Further full/race verification is recorded by the coordinator after integration.

## In-process measurements

Command: `GOCACHE=/tmp/datatree-go-cache DATATREE_ACCEPT_MIB=SIZE DATATREE_ACCEPT_FORMAT=FORMAT go test ./internal/server -run '^TestLargeFileAcceptance$' -count=1 -v`.

| Generated fixture | Initial parse/index | Tree | Native preview | No-match search | Variable-length local action | Undo |
|---|---:|---:|---:|---:|---:|---:|
| JSON 50 MiB, 888622 nodes | 4.032 s | 0.536 ms | 4.53 ms | 354.6 ms | 28.4 ms | 18.0 ms |
| JSON 100 MiB, 1777242 nodes | 8.640 s | 0.760 ms | 7.88 ms | 828.7 ms | 70.9 ms | 46.7 ms |
| JSONL 100 MiB, 1780261 nodes | 8.608 s | 2.214 ms | 1825.1 ms | 834.5 ms | 49.2 ms | 45.1 ms |
| CSV 100 MiB, 1030036 nodes | 7.317 s | 1.774 ms | 1472.2 ms | 564.2 ms | 23.8 ms | 39.2 ms |
| YAML 100 MiB, 1738932 nodes | 11.891 s | 0.486 ms | 26.4 ms | 665.3 ms | 82.4 ms | 32.2 ms |

These are real handler timings in-process. They are not native-browser measurements. In the 100 MiB JSON run, JSON pretty representation took 2.301 s and YAML preview's transient full conversion took 14.826 s. Bounded output alone is not a claim of bounded conversion peak memory. YAML initial import missed the 10-second target. A later parity-tested optimization separates actual YAML metadata from conservative flow-context markers, permitting metadata-free flow YAML JSON previews to stream without materialization; its large-file timing has not been rerun in this table.

## Remaining architectural boundaries

- Initial compact import still builds a full raw parser tree. JSON/JSONL/YAML now emit compact inputs directly during its validated traversal, avoiding the complete application Node graph; CSV still temporarily builds that graph. Parser references are released before retained chunk construction. Direct parser-to-record streaming and peak-memory qualification remain unfinished.
- One flat Source copy is produced for each applied version, as allowed by the coordination contract. Very large individual scalar contexts still require parsing/copying that scalar; the parser does not tokenize incrementally within a single scalar.
- General multi-patch/whole-source changes and unsupported CSV row/YAML contexts may reparse the document. JSON key and structural parent contexts are local, but a root-sized container still requires root-sized parsing. The safe local paths do not silently weaken format validation.
- Advanced queries and raw-node conversions/diff/schema work can transiently allocate a full graph. They do not retain it after completion.
- Variable-length source geometry uses a full UI refresh. Geometry-aware incremental cache invalidation is a separate frontend step.
- Default document limits remain unchanged. Browser responsiveness, initial peak versus retained allocation, and final 10/50/100 MiB qualification remain distinct evidence requirements.
