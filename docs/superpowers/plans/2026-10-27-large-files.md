# Responsive large-file implementation

Goal: responsive editing, search and navigation for JSON, JSONL, YAML and CSV up to 100 MiB. Initial indexing and whole-document changes may take seconds, with visible progress/cancellation. Preserve exact values, YAML preservation, query semantics, draft guards, local assets, and existing interactions.

## Tasks

1. Add reproducible 10/50/100 MiB fixtures, backend benchmarks and opt-in sample measurements; record baseline and resulting performance.
2. Replace unbounded source/inspector DOM with bundled CodeMirror 6, lazy representations and bounded previews; preserve selection, keyboard/IME, folding and drafts.
3. Remove JSON's second YAML parse and redundant span scans/key marshaling; add indexed source lookup and patch-based source mutations/history where safe, preserving non-JSON semantics.
4. Make state metadata-only; add revision-scoped source, lookup, ancestors and representation APIs; isolate expensive session work from global session lookup and support cancellation/progress.
5. Integrate incremental frontend refresh, cancellable search, indexed navigation, original/diff caching; run functional, race, browser and performance checks. Keep default limits until 100 MiB acceptance is demonstrated.

## Coordination contract

- `/api/state` and mutation responses: metadata (`revision`, `root`, `nodes`, `format`, filename/history/schema/limits), no `source` field.
- `GET /api/source?revision=N[&start=S&end=E][&original=true]`: UTF-8 text, normalized browser UTF-16 slice bounds when supplied. Revision header `X-Document-Revision`; source spans API uses normalized UTF-16 coordinates, existing exact bytes retained for export.
- `GET /api/locate?revision=N&start=S&end=E` returns `{revision,range:{id,parent,index,start,end},ancestors:[{id,offset}]}` for smallest containing node (or null).
- `GET /api/node-location?revision=N&node=ID` returns same range/ancestors contract for selected node.
- `GET /api/representation?revision=N&node=ID&format=native|json|yaml&full=true|false`: `{revision,text,truncated,totalBytes,format}`. Default bounded 64 KiB preview; full is explicit. Do not use truncated preview as complete edit content.
- `/inspector`: metadata, actions and representation placeholders only; scalar edit controls remain available. Frontend lazily mounts editors/representations using above endpoint; full subtree Apply retains existing `/actions/edit` semantics.
- Keep `/api/source-map` compatibility for tests/legacy behavior, but frontend must not fetch it for ordinary opens. Tree pages remain `/api/tree`, 20 children, max 50, depth 2.
- `POST /api/patch` body `{revision,patches:[{start,end,text}]}` where bounds are normalized UTF-16 in base revision, non-overlapping and ascending; validates complete result atomically; returns metadata plus `patches` and `previousRevision`. Full-document operations can continue via existing apply APIs.
- Existing `/api/find` gains optional cursor/limit; existing 200-match shape remains compatible if cursor absent. Cancellation propagates request contexts.
- Source-backed document API additions should preserve existing public structs and methods until all callers migrate. Prefer compatibility, verified bounded work, and explicit fallback over silently changing semantics.

## Acceptance

Targets on same machine: post-load typing/scroll/selection p95 <50 ms; cached navigation <100 ms, uncached reveal <300 ms; local edit commit <500 ms; sample open <2 s and 100 MiB open <10 s; search first page <500 ms sample /2 s at 100 MiB. These are targets, not current claims. Complex YAML reparses/whole-file work may exceed interaction targets but must not block the UI.

Run Go suite, race detector, vet, JS tests, reproducible asset build and real browser verification when available. Test UTF-16/CRLF, exact numbers, duplicate keys, invalid/stale/cancelled operations, YAML aliases/comments/streams, CSV quoted newlines, JSONL records, history, bounded memory/DOM and concurrent sessions. Do not commit the private sample or unrelated workspace edits.
