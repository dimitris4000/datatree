# Search and Query Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Find applied document nodes by literal search and bounded path queries, and reveal results in all editor panes.

**Architecture:** Evaluate against the existing ordered Go AST through read-only revision-scoped endpoints. A focused frontend module owns panel state and stale response protection; app.js supplies guarded tree/source/inspector navigation using the existing source map.

**Tech Stack:** Go standard library, existing YAML AST, native JavaScript and Node test runner.

**Spec:** docs/superpowers/specs/2026-10-27-search-and-query-design.md

## Global Constraints

No additional dependencies, no alias expansion, no document mutations. Maximum 200 results, document order, explicit truncation, revision checks. Source drafts block result navigation; inspector drafts retain confirmation. Supported query grammar is exactly the design subset.

## Review Focus

- Escaped and empty keys must navigate without string evaluation.
- Numeric filters must compare large integers and exponents precisely.
- YAML cycles, streams and complex keys must remain bounded and searchable.
- Late results and document changes must not select stale nodes.
- Results on later pages must open their ancestors and reveal the correct page.

## Task 1: Document search and query evaluator

Files: create internal/document/find.go, query.go and find_test.go.
Interfaces: Document.Find(FindOptions) (FindResult, error), FindOptions{Mode, Text, Keys, Values, CaseSensitive}, FindResult{Matches, Truncated}; matches contain id, path/location, kind and bounded preview.
- [x] Write failing tests for literal key/value scope, Unicode case handling, order/deduplication, empty searches, result limit, aliases/streams/complex keys, grammar rejection, wildcard/recursive paths and typed precise filters.
- [x] Run go test ./internal/document and confirm missing-feature failures.
- [x] Implement ordered traversal, bounded parser/evaluator and exact numeric comparison without eval.
- [x] Run document tests and resolve failures.

## Task 2: Session API

Files: modify internal/server/server.go; create internal/server/find_test.go.
Interfaces: GET /api/find?mode=search|query&text=...&keys=true&values=true&caseSensitive=false&revision=N returns revision plus FindResult.
- [x] Write failing HTTP tests for results, revision required/stale rejection, isolation, malformed query and unchanged source/history.
- [x] Run focused HTTP tests; implement route using existing session protections; rerun.

## Task 3: Find panel and navigation

Files: create internal/server/web/find.js and find.test.js; modify app.js, index.html and style.css.
Interfaces: FindController receives load(options), revision(), navigate(id) and render callback. It clears/invalidate results, ignores late responses and traverses previous/next. revealNode uses existing revealPath with cancellation guards.
- [x] Write failing Node tests for submit/clear/next/previous, late responses, errors and cancelled navigation; reuse real revealPath tests for off-page routes.
- [x] Run Node tests; implement controller and text-only DOM panel, guarded ancestor/page reveal integration and visible mode help.
- [x] Run all JavaScript tests and syntax checks.

## Task 4: Documentation and final verification

Files: README.md, docs/implementation.md and this plan.
- [x] Document grammar, bounds, draft behavior and aliases.
- [x] Run go test ./..., go test -race ./..., go vet ./..., npm test.
- [x] Review against design and inspect fresh diff. Record actual evidence and browser limitations. No commit or publication is requested.
