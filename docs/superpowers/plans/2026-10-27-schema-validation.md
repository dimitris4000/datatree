# Schema Validation Implementation Plan

Execute inline with test-first implementation. Spec: ../specs/2026-10-27-schema-validation-design.md.

## Tasks
- [x] 1. Projection/provenance, schema compilation, structured diagnostics, enum/const suggestions and bounds; document tests and implementation.
- [x] 2. Atomic attachment, clear and explicit validation APIs; lifecycle, state metadata and unlocked evaluation; failing HTTP tests followed by handlers.
- [x] 3. Validation panel/controller, Inspector and tree badges, guarded navigation and stale-response handling; failing JS tests followed by UI integration.
- [x] 4. Documentation, full Go/race/vet/JS checks, review and browser verification boundary.

## Interfaces
CompileSchema([]byte) (*CompiledSchema,error); ValidateSchema(*Document,*CompiledSchema) (ValidationResult,error). POST /api/schema, /api/schema-clear, /api/validate as defined by spec. Results include diagnostics with target IDs/instance paths, marks, suggestions, completeness and revision/schema metadata at HTTP boundary.

## Execution ledger
Baseline: go test ./... and npm test passed (53 JS tests).
Ruling: use the existing isolated temporary checkout in place, preserving its current branch and avoiding commits/pushes; this is the task workspace provided by the harness.
Pre-flight: engine results consumed by API and UI; one wire model will be used consistently. Session schema attachment is independent of document history. The existing global-lock handler needs a narrow unlock/relock boundary for schema work.
Dependency download: ordinary sandbox attempt failed DNS; escalated download requested.

Dependency resolution: approved download completed; validator v6.0.3 and x/text v0.14.0 are vendored. Runtime performs no external schema fetches.

Ruling: add explicit numeric operand/exponent/magnitude limits (4096) before the upstream exact-rational evaluator. Normalizing zero otherwise concealed an enormous exponent that could cause an upstream panic.
Ruling: bound evaluation with a conservative 100000-unit preflight and depth512, shared across all YAML stream roots; count alternatives, reference expansions, required/dependency lists and value comparisons. This can reject work that a successful short-circuit would avoid, but prevents multiplicative error allocation before evaluation.
Ruling: retain at most 200 diagnostic details during collection, while marking all evaluated targets; bound display paths to1024 bytes, messages to4096 and suggestion previews to1024. Truncation never changes node navigation IDs.

## Verification evidence

- HTTP schema tests failed with404 before handlers existed; frontend validation tests failed because validation.js was absent before implementation.
- Review regressions were observed failing for empty property-name keys, custom merge tags, malformed bool/null tags, multiplicative error expansion, dependency-list work and stream-wide budgeting; all pass after fixes. Enormous zero-exponent rejection is covered for schema and instance values.
- Final checks passed: GOCACHE=/tmp/datatree-go-cache go test ./..., go test -race ./..., go vet ./...; npm test (60 passing); node --check for app.js/validation.js/tree.js; git diff --check.
- Independent review confirmed its numeric, scalar-tag and work-budget findings were fixed, with no residual important findings in those fixes.
- CUA browser inventory returned apps:[] and browsers:[]. Live layout/keyboard/source-highlight acceptance remains unverified.
- Work stays uncommitted in the supplied checkout. No push, publication or CUE implementation.
