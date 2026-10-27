# Structural diff

## Intent
Show structural changes between a baseline and the applied working document, with before/after previews and navigation to the affected tree nodes. Compare against the original import by default, or an explicitly selected second file.

## Approaches
The selected approach matches object entries by key and sequence items by index. It is predictable and bounded; inserting or removing an array item can make later positions appear changed. Move detection and identity-based array alignment were considered and excluded because repeated items and absent IDs make matching ambiguous.

## Comparison rules
Compare parsed content, ignoring source whitespace, comments, quoting style, anchor names and object key order. Compare strings and booleans by their parsed values; compare finite numbers exactly, without floating-point precision loss. A type change is one changed entry. YAML tags remain meaningful; aliases compare as references to their structural target locations without recursive expansion, so cycles remain safe. Referenced target changes appear at the target's own location. YAML nonfinite numbers compare by their normalized special value.

Match mapping keys by typed structural identity, including complex YAML keys, without expanding aliases. Compare YAML document streams by document index. JSONL and CSV use their existing parsed array/object representation. Different file formats may be compared using these same structural rules.

An added or removed collection is one change with a bounded collection preview, rather than a separate change for every descendant. Changed collections are traversed for individual changes. Object entries are reported in working document order, followed by removed baseline entries; sequences use ascending index order. A renamed key is a removal and an addition. No move, rename or formatting-only change detection is implied.

## User behavior
Add a Changes control to open a collapsible panel alongside the existing Find controls. The baseline selector offers Original and Compare file. Selecting a second file reads it explicitly and keeps it within the current browser session; it never replaces the working document. Show the baseline filename and format. A failed comparison-file load retains the previous baseline and results with an error.

The panel shows added, removed and changed counts, before/after previews, type labels and paths or source locations. Use explicit status labels as well as distinct colors. Highlight current added/changed nodes in the tree, with a distinct indicator on ancestors that contain changes. Removed nodes are represented in the change list; the nearest surviving parent receives a contains-changes indicator. Previous/Next and clickable entries reveal the working node or nearest surviving parent using the existing paginated tree navigation. Removed entries retain their baseline location and preview; no deleted node is fabricated in the current tree.

Opening the panel computes a fresh diff. While open, recompute after accepted edits, conversion and undo/redo. Ignore late responses and reject stale revisions. An imported or newly opened working document resets the second-file baseline. Closing the panel hides tree highlights. The chosen comparison file lasts only for the current in-memory session and is not part of document undo history.

Compare the applied document even when source or subtree drafts exist. Navigating changes requires source drafts to be applied or discarded and retains the existing subtree-draft confirmation. Diff operations never modify the document, source, revision, undo history or disk files.

## Bounds and implementation choices
Use a dependency-free Go comparison over the existing ordered AST, with session-scoped baseline state and revision-checked API requests under the existing loopback/origin protections. Retain the original format alongside its bytes so original comparisons still work after format conversion. Reuse the existing result revelation and render document content as text.

Cap the displayed change list at 200 entries with an explicit truncation notice. Counts and tree highlights cover the complete comparison when evaluation finishes. Bound traversal, key fingerprinting and numeric comparison work; if a work limit is reached, return an explicit error rather than claim a complete diff. Apply the existing 10 MiB, node and depth limits to comparison files. Store at most one additional baseline per session, subject to the existing session expiry/eviction policy. Scalar previews remain bounded. No new dependencies or network services.

The panel arrangement, 200-entry display cap, full-comparison counts/highlights and baseline retention policy are proposed implementation choices. Exporting patches, reverting individual changes, Git comparisons and move detection are outside scope.

## Validation
Add regression coverage for nested additions/removals, type changes, index-based array shifts, reordered keys, exact numbers, equivalent cross-format values, escaped and complex keys, YAML tags/aliases/cycles/streams, original-format retention, result/work limits, API revision/session isolation and failed baseline replacement. Frontend tests cover tree indicators, safe preview rendering, pagination, removed-node navigation, pending drafts and stale responses. Update README and implementation records. Run Go tests, race tests, vet and the JavaScript suite. Report browser verification separately if unavailable.
