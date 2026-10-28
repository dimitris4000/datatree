# Explore & Transform backlog

Status: Ideas for future discussion; implementation not approved.

## Purpose and current baseline

Make complex files easier to navigate, search, understand, and edit. This is a
feature backlog for later requirements and design, not an implementation spec.

Data Tree currently provides literal search across keys and scalar values,
bounded path queries, result navigation, schema validation, and structural diff
with per-change revert. The ideas below extend those capabilities across JSON,
JSONL, YAML, and CSV; exact format support remains a design decision.

## Feature ideas

| # | Feature | Description and benefit |
| --- | --- | --- |
| 1 | Search as you type | Update matches while typing and cancel outdated searches. Locate nodes without repeatedly pressing Find. |
| 2 | Advanced jq queries | Support compound conditions, pipelines, and transformations, such as finding services with high replica counts and no resource limits. Express questions beyond simple path filters. |
| 3 | Search from selection | Offer actions such as “Find this value,” “Find this key,” and “Find records with this field.” Turn a selected attribute into a search without writing syntax. |
| 4 | Suggested query refinements | Suggest filters based on the selected field's type and observed values, such as status equals failed or duration exceeds a threshold. Help users discover useful questions about unfamiliar data. |
| 5 | Search within a subtree | Restrict searches to the selected branch and clearly display the scope. Reduce noise from unrelated file sections. |
| 6 | Visual filter builder | Build AND/OR conditions through field, operator, and value controls, with an equivalent query preview. Make complex searches accessible without memorizing syntax. |
| 7 | Query autocomplete | Suggest actual property names, paths, operators, and values while typing. Reduce mistakes in deeply nested or inconsistently named data. |
| 8 | Explain query results | Show why each node matched and highlight fields satisfying its conditions. Help verify that complicated queries mean what the user intended. |
| 9 | Progressive result narrowing | Search within the current result set and step back through refinements. Support exploration when the right query is not known upfront. |
| 10 | Matching branches view | Temporarily show only matches and their ancestor paths, with nearby context expandable. Make sparse matches understandable inside a huge tree. |
| 11 | Record table view | Display matching objects as rows with selectable nested fields as columns; cells navigate to original nodes. Make repeated records easier to compare and inspect. |
| 12 | Field value summaries | Show distinct values, counts, missing values, and type distributions for a chosen field. Reveal patterns and anomalies before searching or editing. |
| 13 | Find similar structures | Find objects with the same or similar field layout as the selected object. Locate related records across different branches despite inconsistent values. |
| 14 | Missing and inconsistent field finder | Find absent properties, unexpected types, and empty values across comparable records. Uncover data-quality problems even without a schema. |
| 15 | Duplicate detection | Find duplicate records or repeated identifiers using user-selected fields. Investigate collisions without treating every repeated scalar as a problem. |
| 16 | Related-record navigation | Connect an identifier field to a reference field and jump between matching records. Traverse relationships in large configuration files or exports. |
| 17 | Bookmarks and navigation history | Pin important nodes and provide Back/Forward through visited locations. Preserve orientation while moving between distant branches. |
| 18 | Saved and parameterized searches | Save named queries with inputs such as service name or environment. Repeat investigations without editing long expressions. |
| 19 | Bulk edit matching nodes | Replace values, rename keys, or remove fields across selected matches, with a before/after preview and one Undo action. Handle repetitive corrections safely. |
| 20 | Transformation workspace | Preview computed changes such as deriving fields, restructuring objects, or normalizing values before applying them. Enable substantial edits while keeping generated output separate from navigable source matches. |

## Suggested starting features

These are recommendations, not an agreed implementation order:

1. **Search as you type:** improve the existing search workflow.
2. **Search from selection:** connect exploration directly to searching.
3. **Bulk edit matching nodes:** turn reviewed matches into repeatable corrections.

Together these support selecting an example, finding related nodes, reviewing
changes, and applying them.

## Open decisions for later planning

- **Query language:** choose full jq compatibility, a documented subset, or an
  extension of existing path queries. Decide how users choose between languages.
- **Result identity:** distinguish queries returning existing nodes from queries
  producing computed values. Direct navigation and editing require source-node
  identity; generated results need a separate output preview.
- **Format support:** define behavior for JSONL records, CSV string cells, YAML
  streams, aliases, merge keys, tags, and complex mapping keys.
- **Large files:** define cancellation, work limits, pagination, and clear
  incomplete-result reporting. Search must cover the applied document rather
  than only rendered nodes.
- **Editing safety:** define match selection, preview, atomic application,
  stale-revision checks, draft handling, source preservation, and Undo/Redo.
- **Persistence:** decide where bookmarks, saved searches, parameters, and
  relationship mappings live and how they behave after document edits or reloads.

To resume, select a feature by number, confirm its user workflow and acceptance
criteria, and resolve its relevant open decisions before implementation.
