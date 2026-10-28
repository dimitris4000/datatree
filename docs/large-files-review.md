# Large-file implementation review

Final source review of the working diff and compact contract. Existing tree pagination edits were preserved. No additional heavy benchmarks or full suites were launched by this review.

## Final status

No remaining confirmed high-impact finding in the reviewed code. Qualification limitations below remain explicit.

## Resolved findings

- Browser qualification subsequently found premature Undo/Apply enablement during canonical inspector restoration. Busy-state guards now remain active until completion; a delayed-inspector regression reproduced the lost-click window before the fix.

- Empty sole-column CSV action values are quoted, while raw source deletion falls back to full parsing so blank records are skipped consistently. Regression coverage checks both paths, context expansion and materialization parity.

- Inspector writes now stop the untracked htmx XHR and use tracked `/api/edit` jobs. Cancellation acknowledgement is followed by terminal job polling, so an immediate unchanged state read is no longer treated as proof of cancellation.
- Canonical refresh detaches cancellation after mutation metadata arrives; committed refresh failures are explicit.
- Read-only representations expose full-load actions and copy uses explicit truncation state, including after failed full-load requests.
- Legacy geometry-preserving incremental copies use thin identity-only ancestor chains, removing predecessor raw/Children graph retention.

## Compact review

Compact versions use immutable source pieces and chunked records with ID parent references rather than predecessor application/raw graphs. Replacement allocates/reserves descendant IDs before remapping, reuses deleted slots, preserves target identity/key weight, propagates counts/weights/heights and refreshes affected ancestor anchors. Shared source anchors resolve against each version's immutable index. Session history retains compact tree/text versions and clears popped/evicted slots.

The adapter discards retained legacy Node/raw graphs after compaction. Lookup/tree pages, source navigation, simple queries and ordinary Find use compact records. Explicit complex schema/diff/conversion paths materialize temporary legacy documents and remap IDs by topology; server inspector root checks now compare IDs. YAML local replacement conservatively falls back around metadata, aliases/anchors, flow context and multiline scalar source. No additional high-impact source/UTF-16/CRLF/alias/cancellation issue was identified in this review.

Stable-ID frontend refresh remains gated on applied source draft, consecutive revision, matching root and server stable geometry. It preserves independent child page offsets and source-location caches only for that geometry-preserving case. Variable geometry uses the full canonical UI refresh.

## Qualification boundaries

Automatic compaction starts above 1 MiB and initial parsing still builds a temporary raw parser tree (and, for CSV, an application graph), so retained-memory reduction does not establish bounded initial peak allocation. Local scalar/context updates exist across formats, while unsafe metadata/structural and complex operations retain explicit full fallback. Contiguous current source and some directory/array copies remain proportional to document size.

Native-browser profiling and 10/50/100 MiB measurements are being performed separately by the coordinator. Their latest evidence must determine acceptance; this source review does not claim those performance gates. Keep default limits until qualification supports a change.

## Startup-path follow-up, 2026-10-28

Independent read-only review found no confirmed high-impact issue in direct JSON/JSONL/YAML compact import, local replacement or session snapshots. IDs, aliases, metadata, key weights and source spans preserve prior compaction semantics. Dense import IDs use direct record/visit arrays; sparse/reordered IDs retain map validation, and root/child/alias bounds are checked before slice access. The trailing-trivia YAML concern is guarded by local-context checks and complete-parse fallback. Browser and test evidence is recorded separately in the performance guide and ledger.
