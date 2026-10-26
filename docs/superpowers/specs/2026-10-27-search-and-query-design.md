# Search and path queries

## Intent
Help a local user find keys and values and navigate large JSON/YAML documents without manually expanding branches. Reuse the existing tree, source and inspector selection flow.

## Approach
Use a bounded built-in query evaluator over the ordered document AST. This is the selected approach instead of a full JSONPath dependency: a smaller implementation with explicitly documented syntax limits. No external service or dependency is required.

## User behavior
Add a compact Find panel above the tree with Search and Path/query modes. Search is literal substring matching, case insensitive by default, with case sensitivity and Keys/Values controls. Search keys and scalar values across the entire applied document, including collapsed and off-page nodes. Empty search clears results.

Path/query mode accepts `$`, dot properties, quoted bracket properties (including escaped keys), nonnegative array indexes, `.*`/`[*]`, recursive property lookup (`$..name` or `$..['name']`), and array filters such as `$.services[?(@.replicas > 3)]`. Filters compare a relative property/index path against a string, number, boolean or null literal using `==`, `!=`, `<`, `<=`, `>` or `>=`; ordered comparisons require matching number or string types, and missing properties do not match. Numeric comparisons retain precision. Unsupported syntax produces an actionable error rather than executing code. Functions, scripts, unions, slices, regex and compound predicates are outside this version.

Results are in document order, deduplicated, and show a path or source location, node type and short preview. Clicking a result opens ancestors, loads the required pages, centers the node and updates the inspector and synchronized source highlight. Previous/Next controls traverse results. Preserve existing pending inspector edit confirmation. Source drafts must be applied or discarded before navigation; the panel explains that searches use the applied document.

## Bounds and format behavior
Evaluate server-side against the complete current AST, with revision checks, bounded query length and traversal work. Return at most 200 results with an explicit truncation notice. Do not expand YAML aliases while searching or querying, preventing cycles. Search works for all supported formats. Queries operate on mappings and sequences; YAML streams and complex keys remain searchable through locations but unsupported path shapes receive no fabricated JSONPath. No document mutation, history entry or file write occurs.

## Integration and validation
Use read-only session-scoped search/query endpoints with the existing loopback protections. Invalidate results after document revision changes and ignore late responses. Render result content as text. Keep the existing source map for ancestor/page revelation.

Add meaningful tests for search coverage, query syntax and rejection, escaped keys, filters and precise numbers, aliases, limits, revision/session isolation, off-page revelation and stale responses. Update README with supported syntax and limitations. Run Go tests, race tests, vet and JavaScript tests; report browser verification separately if unavailable.

## Implementation choices
The Find panel location, explicit submit behavior, 200-result cap and initial filter subset are proposed implementation choices. Persistent search history, replacement and schema validation are outside scope.
