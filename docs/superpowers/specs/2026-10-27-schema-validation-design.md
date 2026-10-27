# Schema validation

Approved design: offline JSON Schema validation of applied JSON/YAML, with explicit Validate, affected-node errors and read-only allowed-value suggestions. CUE support is deferred.

## Approach
Use the Go jsonschema/v6 v6.0.3 library, vendored for offline runtime. Default to draft 2020-12 and honor supported standard dialects. Permit references to resources embedded in the uploaded schema; reject filesystem/network loading. Keep draft-specific format behavior and disable optional content decoding. Accept JSON object/boolean schemas; reject duplicate keys and unsupported dialects.

Project a fresh JSON-compatible value from the ordered AST with exact numbers and provenance. Validate each YAML stream document separately. Resolve aliases and merges with current precedence; errors inside expanded aliases target the occurrence, and inherited merge values target the receiving merge entry. Missing-property errors target the object and unexpected properties target their values. Timestamps/binary use their scalar text. Complex/non-string keys, custom tags, nonfinite numbers and cyclic references produce explicit cannot-validate diagnostics, never a valid result.

## Interaction and lifecycle
One schema per session document, attached by upload or paste. Attachment compiles but never runs validation. Validate checks the last applied document, including with pending drafts. List errors and allow Previous/Next navigation using existing draft guards, pagination and synchronized source selection. Show direct/contains error badges independently of diff badges, and selected-node errors/suggestions in Inspector. Suggestions come from enum/const constraints, retain branch identity and are not automatic edits or promises that every other constraint is satisfied.

Applied edits, conversion and Undo/Redo invalidate results and remove badges; only explicit Validate recomputes. Retain schema across these operations and reset on successful import/open. Reload restores attachment metadata. Failed schema replacements preserve prior attachment/results. Closing hides badges; late responses cannot restore stale results. No source, revision, undo or disk changes from validation.

## API and bounds
POST /api/schema {revision,filename,text}; POST /api/schema-clear {revision}; POST /api/validate {revision,schemaToken}. State includes attachment metadata. Enforce existing origin/session guards. Compile and validate outside the global lock, rechecking session identity/revision/schema generation before publishing. Schemas: 1 MiB, 10,000 nodes, depth 64. Expanded instances use existing document limits. Display at most 200 diagnostics and 50 suggestions per diagnostic with truncation flags; incomplete projection/evaluation never means valid. Escape all content through text rendering.

## Verification
Test constraints, compositions, references, boolean schemas, exact numbers, escaped keys, YAML streams/aliases/merges, unsupported values, limits, lifecycle, atomic replacement, session/revision isolation, safe rendering, late responses, source navigation and coexisting badges. Run Go tests/race/vet, npm test and diff checks. Browser layout/keyboard/source acceptance must be reported separately.

## Alternatives
Browser validation would introduce a second document representation; a handwritten validator would limit compatibility. Native CUE is reserved for a future feature rather than translating JSON Schema through CUE now.
