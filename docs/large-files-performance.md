# Large-file verification

The private example is never copied into repository fixtures. Generated fixtures contain nested collections, exact large numbers and Unicode; CSV also contains quoted newlines. Byte budgets end at a complete record and can be slightly smaller than the selected size.

## Commands

Generate and exercise a 10, 50 or 100 MiB document through real in-process HTTP handlers:

```sh
DATATREE_ACCEPT_MIB=100 DATATREE_ACCEPT_FORMAT=json go test ./internal/server -run '^TestLargeFileAcceptance$' -v -count=1
```

Repeat with `jsonl`, `yaml` and `csv`. The test checks metadata, source retrieval, root tree/inspector, bounded previews, indexed location, absent-text search, scalar replacement and exact-byte Undo. It logs times and response lengths, never private values.

Read a private file directly for the same acceptance workflow:

```sh
DATATREE_ACCEPT_FILE="$HOME/Downloads/state.json" go test ./internal/server -run '^TestLargeFileAcceptance$' -v -count=1
```

Backend time and allocation measurements:

```sh
DATATREE_BENCH_FILE="$HOME/Downloads/state.json" go test ./internal/document -run '^$' -bench '^BenchmarkLarge' -benchtime=3x -benchmem
DATATREE_BENCH_MIB=100 DATATREE_BENCH_FORMAT=json go test ./internal/document -run '^$' -bench '^BenchmarkLargeParse$' -benchtime=1x -benchmem
```

`DATATREE_BENCH_FILE` is JSON; generated benchmark formats support all four formats. Use an available writable `GOCACHE` when the normal cache is restricted. Run performance commands sequentially without competing heavy jobs; report hardware, input structure and whether source indexes are cold. `B/op` measures cumulative allocations, not peak resident memory. A successful workflow is functional evidence, not proof that latency targets passed.

## Evidence and release boundary

Before changes, the local 7,654,398-byte JSON example took 1.323 s to parse and 1.462 s for scalar replacement in single-iteration Go benchmarks on an Intel i9-9980HK. Parsing allocated 909 MB cumulatively; replacement allocated 1,063 MB. These measurements exclude browser rendering.

An intermediate implementation reduced those times to 0.516 s and 0.467 s, with 177 MB and 185 MB allocated. Further parser changes are measured separately in the implementation ledger; do not treat a single iteration as a p95 result.

All generated 10 MiB format workflows passed. A local sample workflow produced a 1,922-byte root inspector and capped each representation at 64 KiB before JSON response escaping. Native preview, metadata and paginated tree requests no longer return whole rendered documents. Initial index costs are paid once per immutable document revision.

## Chromium qualification

Install the browser once with `npx playwright install chromium`, then run:

```sh
DATATREE_BROWSER_MIB=100 DATATREE_BROWSER_FORMAT=json DATATREE_PERF_STRICT=1 npm run test:browser
DATATREE_BROWSER_FILE="$HOME/Downloads/state.json" DATATREE_PERF_STRICT=1 npm run test:browser
```

Repeat generated checks for `jsonl`, `yaml` and `csv`; sizes 10/50/100 are supported. The harness builds a temporary server, exercises bounded rendering, typing, scrolling, query/reveal, source Apply and exact-byte Undo, then removes its generated source/server files. Private-file runs exercise rendering/input without committing edits. No screenshots, video or traces capture private content. JSON timing reports are written under the operating system temporary directory as `datatree-browser-results-FORMAT-SIZE.json`.

Open time is server initial load/index time plus navigation-to-revision-ready time; it excludes fixture generation, binary compilation, browser launch and harness readiness polling. Typing p95 uses Chromium Event Timing keydown durations; events omitted below its 16 ms threshold are conservatively counted as 16 ms. First-frame and two-frame timing are recorded separately, as are scroll-frame p95 and longest main-thread tasks. These are one-run measurements on an Intel i9-9980HK, not statistically established service guarantees. JavaScript used heap is recorded separately and is not backend peak RSS. Native IME, accessibility and other browsers remain manual checks.

The private 7,654,398-byte example opened/rendered in 1.256 s, with typing p95 32 ms and 74 rendered source lines. These generated fixture runs exercise edits and Undo as well as rendering:

| Fixture | Open/render | Typing p95 | Scroll frame p95 | Query/reveal | Edit commit | Rendered lines |
|---|---:|---:|---:|---:|---:|---:|
| JSON 10 MiB | 1.457 s | 40 ms | 26.9 ms | 224 ms | 242 ms | 74 |
| JSONL 10 MiB | 1.376 s | 24 ms | 14.6 ms | 135 ms | 229 ms | 74 |
| YAML 10 MiB | 1.762 s | 32 ms | 11.2 ms | 126 ms | 245 ms | 74 |
| CSV 10 MiB | 1.578 s | 24 ms | 8.6 ms | 142 ms | 221 ms | 74 |
| JSON 50 MiB | 4.097 s | 40 ms | 30.7 ms | 174 ms | 234 ms | 74 |
| JSONL 50 MiB | 4.154 s | 32 ms | 13.4 ms | 138 ms | 219 ms | 74 |
| YAML 50 MiB | 5.608 s | 32 ms | 10.5 ms | 146 ms | 238 ms | 76 |
| CSV 50 MiB | 4.625 s | 32 ms | 9.4 ms | 145 ms | 229 ms | 74 |
| JSON 100 MiB | 7.167 s | 40 ms | 32.8 ms | 188 ms | 234 ms | 74 |
| JSONL 100 MiB | 7.392 s | 32 ms | 14.6 ms | 137 ms | 226 ms | 74 |
| YAML 100 MiB | 10.394 s | 32 ms | 30.3 ms | 173 ms | 254 ms | 83 |
| CSV 100 MiB | 9.598 s | 32 ms | 10.0 ms | 132 ms | 217 ms | 74 |

All 10/50 MiB strict checks passed. At 100 MiB, JSON/JSONL/CSV passed; YAML completed the functional workflow but failed the 10-second open threshold at 10.394 s. Its typing, scrolling, reveal and local edit thresholds passed. These single runs occurred sequentially during qualification; startup optimizations landed between the earliest and latest 100 MiB runs, so they are not a controlled format ranking. Long tasks during initial editor installation reached about 0.7 s at 100 MiB; post-load typing measurements do not imply that initial installation is frame-perfect.

## Engine boundaries

Documents above 1 MiB retain compact persistent records rather than raw YAML/application node graphs. Source anchors and record chunks are shared between versions. Simple search, direct queries, source navigation and bounded child pages operate directly on records. Local scalar edits across formats and JSON/JSONL parent-context changes avoid complete reparsing; history restores compact versions without parsing. JSON/JSONL/YAML initial imports emit compact inputs while walking the validated parser tree, omitting the complete application graph and eager path/location strings. CSV still constructs a temporary application graph before compaction.

Initial parsing still allocates a complete raw parser tree and compact input slice. Raw parser references are released before retained chunk construction. Full conversions, schema evaluation, diff/revert, advanced queries, broad contexts and unsupported CSV/YAML edits may transiently materialize or reparse the complete document. Those operations remain cancellable background work; arbitrary operations are not guaranteed to complete within local-edit targets. A contiguous current source copy is still produced on Apply. Variable-length edits refresh the canonical UI; only guaranteed unchanged source geometry uses targeted cache refresh.

Keep default 20 MiB / one-million-node limits until peak-memory and manual interaction qualification support a change. Browser timing passes do not establish bounded initial peak memory.

## Initial memory measurement

A fresh standalone generated 100 MiB YAML parse benchmark on the same Mac completed in 7.950 s, allocating 2,464,197,712 bytes cumulatively. `/usr/bin/time -l` reported maximum RSS 2,093,969,408 bytes (about 2.0 GiB). This includes benchmark fixture construction and process/runtime overhead; it is not retained document size or a browser-process measurement. It confirms that initial transient memory remains substantial even though compact documents and history discard parser graphs.

```sh
go test -c -o /tmp/datatree-document-final.test ./internal/document
DATATREE_BENCH_MIB=100 DATATREE_BENCH_FORMAT=yaml /usr/bin/time -l /tmp/datatree-document-final.test -test.run='^$' -test.bench='^BenchmarkLargeParse$' -test.benchtime=1x -test.benchmem
```

Remaining performance work: direct parser-to-record import (especially YAML/CSV), lower transient initial allocation, the missed 100 MiB YAML open target, and manual IME/accessibility/cached-navigation qualification. File limits remain unchanged.
