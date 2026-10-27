# Data Tree

A local Go web application for exploring and editing JSON, JSONL, YAML and CSV as a left-to-right tree. Each depth shares a column. Object cards group scalar fields, while nested collections branch into separate cards.

## Run

Requires Go 1.23 or newer. Go dependencies and htmx 1.9.10 are vendored.

```sh
go run ./cmd/datatree
```

Open **http://127.0.0.1:8080**. Use **Load example**, **Open file**, or paste source text and choose **Apply source**. An alternative port is available with `-listen 127.0.0.1:8090`. Listening on non-loopback addresses is rejected.

Pass an optional file argument to load it at startup:

```sh
go run ./cmd/datatree -listen 127.0.0.1:8090 ./data.yaml
```

For a compiled binary, use `datatree [flags] [file]`. JSON, JSONL/NDJSON, YAML/YML and CSV are supported. Paths are relative to the process's working directory unless absolute; quote paths containing spaces. Put flags before the file argument. Missing or unreadable files, directories, unsupported extensions, invalid UTF-8, oversized files and malformed documents stop startup before listening, with a nonzero exit status and an error naming the file and cause. Each new browser session starts from the validated startup snapshot with independent edits/history. Omitting the file starts an empty workspace.

The default document limits are **20 MiB** (20 × 1,024 × 1,024 bytes) and **1,000,000 parsed nodes**. Configure them independently before the file argument:

```sh
datatree -max-file-size-mb 40 -max-nodes 2000000 ./data.json
```

Both flags require positive values. These settings apply to CLI startup, WebUI opening/imports, source/subtree edits, conversion, Undo/Redo and comparison files. A parsed-node-limit error concerns document structure, not its file size: mapping keys also count as parsed nodes, so increase `-max-nodes` for dense documents. Schema files retain their independent 1 MiB and schema-complexity limits.

Opening `XXX.YYY` by CLI argument or the WebUI’s local path also checks for `XXX.schema.json` in that directory. If found, it is attached automatically; press **Validate** to run it. Missing companions are optional. An existing unreadable, oversized or invalid companion stops startup, or rejects the WebUI open while retaining the previous document/schema. Schemas are limited to 1 MiB and use the normal offline schema rules.

**Open file** uses a custom file browser backed by the running CLI process. Browse directories, select a supported file, then choose **Open**, or double-click the file. The picker fills in its full server path and automatically loads a matching schema. **Up**, the editable directory path and **Show hidden files** help navigate. It starts beside the currently opened local file, or in the CLI’s working directory. Files are on the computer running Data Tree, which may differ from the computer displaying the browser. You can also enter an exact file path directly.

### Enable p5.js

The project uses a p5.js canvas when p5 is available, with a native Canvas 2D fallback for an offline checkout. This development environment could not download p5.js, so its distribution is **not included**. To install the pinned p5.js 1.11.11 distribution:

```sh
./scripts/vendor-p5.sh
```

Restart/rebuild the Go server afterward: frontend assets are embedded at build time. The script downloads only the library and license from public distribution URLs; application files are never sent externally. Once vendored, the entire app runs without network access. The same layout, drawing, hit detection and interactions serve both rendering backends. Real p5/browser integration still needs verification in a browser-enabled environment.

## Use

The compact top toolbar combines file opening, document identity and editing controls. Status and error messages appear in the bottom footer.

The toolbar has one **Open file** button. Its dialog offers the server’s file browser and an editable file path. Both use the CLI process’s filesystem permissions and preserve Git context. Tracked files show the current branch and short HEAD commit beside the filename; hover for the full hash. Detached checkouts show **Detached HEAD**. Git details refresh with state responses (including page reloads and edits), and describe the repository checkout, not a commit of your in-memory edits. Untracked files, files outside a repository, unborn repositories and systems without Git show no Git badge. Edits stay in memory until a copy is saved using **Download** or the conversion dialog.

- **Collapse all** in the tree controls collapses the selected node and every descendant, including hidden branches and other pages. Other branches retain their expansion state. With no selection, it collapses the entire tree.
- **Source** on the left contains editable working text, line numbers, a line/column indicator and a read-only Original tab. Selecting a tree card or field highlights and scrolls to its source; moving the editor cursor or selecting text selects the smallest enclosing node in the tree and inspector, opening ancestors and loading the required pages. Selection stays in the editor while the inspector updates. Sync pauses for unapplied source drafts and for Original text that differs from the working document; apply or discard the draft to resume. Apply explicitly; invalid source stays in the editor and the tree retains its last valid state.
- **Tree** supports drag-to-pan, wheel/button zoom, fit, and expand/collapse using the triangle on a collection card. Selection uses a strong blue outline that stays visible when zoomed out. Selected attributes also have a blue row fill, border and left stripe; Changes and Validation labels remain visible. Double-click a collection title to collapse it when expanded, or open its next level when collapsed. Select a collection to show the level input and **Expand X** button in the bottom controls; X levels are counted beneath that node, and existing expanded branches stay open. Expansion uses each collection’s current page (20 children) and stops after 1,000 collections per action. Collections show 20 children per page. Use the footer to page through fields or items. The keyboard node list below the canvas marks the selected node and brings it into view; attribute navigation centers the actual row, including in tall cards. Pane dividers also accept arrow keys.
- Right-click a card or field for **Duplicate**, **Rename**, and **Delete**. Object entries ask for a new key; array items duplicate directly. Delete requires confirmation and can be undone. Actions that do not apply to a node are disabled. Escape or clicking outside closes the menu. From the keyboard node list, press **Shift+F10** (or the menu key), use arrow keys to choose an action, and press Enter.
- Click a card header to select its subtree, or an object field to select that value. The **Inspector** shows its location, native subtree editor, JSON/YAML representations, structured value editing and collection insertion. Duplicate within the same parent; object duplicates require a unique key. Use the triangles inside JSON/YAML representations to collapse nested blocks, or **Collapse all** / **Expand all**. In Subtree, choose **Foldable view** to browse nested blocks and **Edit text** to return to editing. Folding never changes the full text used by Copy or Apply. Copy text or a location using the Copy buttons.
- Apply or discard source drafts before mutations. Pending subtree edits prompt before selection changes. Invalid subtree input remains editable. All accepted changes are reversible with Undo/Redo.
- **Convert…** opens a preview; **Confirm conversion** updates the working source. For a file opened by local path or a startup argument, select **Save beside the original file** to save the converted copy in that directory. The suggested name keeps the basename and uses the target extension (`data.json` → `data.yaml`); edit it before choosing **Convert and save**. Existing files are preserved: choose another name if one already exists. A failed save keeps the preview open and the working document unchanged. Browser uploads offer **Download converted copy** with the same editable filename suggestion; the browser controls its destination because upload directories are hidden. Undo restores the previous working format/source and leaves saved copies on disk. JSON-to-YAML conversion uses block mappings, dash-prefixed lists and two-space indentation, matching the Inspector’s YAML representation. Strings are quoted only when needed, and numeric precision and key order are retained. **Download** also saves the working file as a copy.

## Format rules

| Format | Representation and constraints |
| --- | --- |
| JSON | Ordered mappings; exact number text is retained, including large integers. Duplicate keys and trailing data are rejected. |
| JSONL / NDJSON | An ordered list, one JSON value per nonblank line. Editing must keep an array at the root. Export writes one compact value per line. |
| YAML | Document streams, complex keys, comments, styles, tags, anchors and aliases. Aliases appear as references and can be followed; they are not recursively expanded in the tree. |
| CSV | A required, nonempty, unique header row; consistent row widths; string-valued cells. Edits must retain the original column names/order and row shape. |

A scalar field's subtree is its value; its key belongs to the parent object. The Inspector provides copyable JSONPath locations, which can also be pasted into the Find panel’s Path / query mode. Keys use escaped bracket notation. YAML document streams and structures without a JSON-compatible path show line/column locations instead.

### Search and path queries

Use **Search** above the tree to find literal text in keys, scalar values, or both. Matching ignores case unless **Match case** is checked. Search includes collapsed branches and other pages, and supports JSON, JSONL, YAML and CSV. It searches parsed content, rather than source comments or formatting; complex YAML keys are searched through their scalar parts. Alias values are searchable as `*anchor` references without following them.

Switch to **Path / query**, enter an expression and choose **Find**:

| Expression | Result |
| --- | --- |
| `$` | Document root |
| `$.services[0].name` | First service’s name |
| `$['a.b']['']` | Quoted keys, including dots or empty keys |
| `$.services[*].name` | Every service’s name |
| `$.settings.*` | Direct children of a mapping or sequence |
| `$..name` or `$..['name']` | Every descendant property named `name` |
| `$.services[?(@.replicas > 3)].name` | Names of services with more than three replicas |
| `$.services[?(@.name == 'api')]` | Services with that exact name |
| `$[?(@ >= 10)]` | Numeric items of a root array at least 10 |

Quoted keys and string literals accept single or double quotes and JSON escapes; single-quoted strings additionally accept `\'`. Filters accept relative property/index paths and a string, JSON number, boolean or null literal. Supported operators are `==`, `!=`, `<`, `<=`, `>` and `>=`. Equality compares types as well as values. Missing properties never match, including `!= null`; ordered comparisons require two numbers or two strings. Numeric comparisons preserve precision, including large integers and exponents. YAML nonfinite values do not match numeric filters.

This is a bounded query subset. Functions, scripts, slices, unions, recursive wildcards and compound predicates are unsupported. Queries do not expand aliases or resolve merge keys. YAML document streams require Search; complex or non-string mapping keys and their descendants are omitted from path queries.

Results appear in document order, once per node. Select a result or use **Previous** / **Next** to reveal its ancestors and correct page, center the tree and update the Inspector and synchronized Source selection. A query with one result navigates immediately. Navigation preserves the existing subtree-draft confirmation and requires source drafts to be applied or discarded. Searching itself uses the last applied document. The Original tab only highlights when its text matches the applied document.

Search and query text are limited to 4,096 UTF-8 bytes. Results are capped at 200 with a notice when more matches exist. Queries also have a 256-step limit and a work budget; numeric filter operands are limited to 4,096 bytes each. Narrow a query if a limit is reached. Results clear when the document changes or the search controls change. Finding and navigating never modify the document or its undo history.

### Schema validation

Open **Validation** to see an automatically attached companion schema, select **Choose JSON Schema** to browse the server with the same custom picker used by **Open file**, or expand **Paste JSON Schema** and choose **Attach pasted schema**, then press **Validate**. The schema picker lists folders and JSON files and reads the selected file with the CLI process’s permissions. Attaching a schema and opening the panel do not run validation. Validation checks the last applied JSON, JSONL, YAML or CSV document; unapplied source and subtree drafts are excluded. CSV validates as an array of row objects keyed by the headers, with every cell remaining a string (including numbers and empty cells). JSONL validates each nonblank record independently against the same schema.

Errors appear in a navigable list, on affected tree cards/fields and their ancestors, in the keyboard node list, and in the selected node's Inspector. Select an error or use **Previous error** / **Next error** to reveal its page, select the node and highlight its source. Existing source/subtree draft guards still apply to navigation. Validation labels coexist with Changes labels. Closing Validation hides its indicators.

Allowed-value previews come from `enum` and `const`, including referenced schemas. Each preview names its schema constraint/branch; it does not promise to satisfy other constraints and never applies edits. Defaults and examples are not suggestions. Property-name constraints describe keys, not replacement field values.

Accepted edits, conversion and Undo/Redo mark results stale and remove active badges. Press Validate to recompute. Schema attachments survive these operations and page reloads. A successful new-file import/open replaces the previous schema with its companion, or clears it if none is available. CLI sessions start with the validated document/schema snapshot, independently of other sessions. Attachments expire with the session. Results are not restored on reload. Failed schema replacements retain the previous attachment/results. **Clear schema** removes the attachment. Validation changes no source, revisions, history or disk files.

Schemas must be JSON objects or booleans. Draft 2020-12 is the default; supported standard `$schema` declarations select drafts 4, 6, 7, 2019-09 or 2020-12. Built-in dialect metadata and resources embedded in the uploaded schema are available offline. External filesystem/network references and custom dialect loading are disabled. `format` follows the selected draft's standard library behavior; optional content decoding/assertions are disabled.

YAML streams validate each document against the same schema. Aliases and merges expand for validation without changing source; errors in expanded aliases target their occurrence, and inherited merge errors target the receiving merge entry. Explicit values override merged values, and earlier merge sources take precedence. Missing properties target their containing object. Timestamps/binary scalars validate as their scalar text. Complex/non-string keys, custom tags, invalid tagged scalars, nonfinite numbers and cyclic references produce explicit incomplete-validation diagnostics rather than a valid result.

Schemas are limited to 1 MiB, 10,000 nodes and depth 64. Expanded instances retain the document's byte/node/depth limits. Validation numbers retain exact precision, with a 4,096-character operand limit, exponent magnitude at most 4,096 and normalized decimal magnitude at most 4,096. A conservative 100,000-unit evaluation-work check (depth 512) counts potential schema/value evaluations, branches and comparisons before running the validator; complex inputs may be rejected even when a particular branch would short-circuit. A work failure means validation is incomplete. The list retains up to 200 diagnostics; tree indicators cover all errors from completed evaluation. Suggestions show up to 50 values per diagnostic, with 1,024-byte previews; instance/schema path previews are also limited to 1,024 bytes, and messages to 4,096 bytes. Truncation is indicated. CUE schemas are reserved for a later feature.

### Structural diff

Choose **Changes** above the tree to compare the applied working document with its **Original** import. **Choose comparison file** uses the shared custom server file picker to load a separate JSON, JSONL, YAML or CSV baseline without replacing your working document. The baseline selector switches between Original and the retained comparison file; the filename and format appear below the controls. Cancelling either panel’s picker leaves its previous selection in place. Failed replacements retain the previous baseline and results.

The panel shows added, removed and changed counts plus selectable **Before** and **After** previews. Drag to select and copy either value; preview text sits outside the action buttons. Choose an entry’s heading button or use **Previous change** / **Next change** to reveal the current node, including collapsed branches and other pages. Removed entries show their baseline path or source location and navigate to the nearest surviving parent. Green **Added** and amber **Changed** labels mark current tree nodes; blue **Contains changes** labels identify ancestors, including parents of removed nodes. The keyboard node list includes the same status labels. Closing Changes hides the indicators.

Comparison follows these rules:

- Match mappings by typed key identity and arrays or YAML stream documents by index. Reordering mapping keys produces no changes. Inserting an array item can show changes at subsequent indexes; moves are not detected.
- Ignore whitespace, comments, quoting styles and anchor names. Compare scalar types and values, retaining exact finite-number equality across JSON/YAML numeric spellings. Nonfinite YAML numbers compare by normalized special value. Custom tags remain meaningful.
- Treat an added or removed collection as one change; added descendants also receive tree indicators. A type change is one changed entry. Renaming a key produces an addition and a removal.
- Compare complex YAML keys structurally. Mapping key types remain distinct, including integer versus float tags. Ambiguous equivalent keys produce an explicit comparison error rather than silently matching the wrong entry.
- Compare aliases by their target’s structural location without expanding them, including cycles. An edit to the target is reported at the target itself; retargeting a reference changes the reference. JSON/YAML comparisons do not expand aliases or resolve merge keys.

Changes refresh after accepted edits, format conversion and undo/redo while the panel is open. Comparing uses the last applied document; navigating and reverting require source drafts to be applied or discarded and retain the existing subtree-draft confirmation. Computing a diff does not alter the working source, revision, undo history or files on disk.

Each entry has **Revert**: remove an added entry, restore a removed entry, or replace a changed value with its baseline value. Revert uses the complete baseline node, including nested content, rather than the truncated preview. Each successful action creates one working revision, supports Undo/Redo, retains the selected baseline and refreshes Changes. The current file format remains in use; Download saves the result as a copy.

Reverts validate the document revision and comparison-file identity before applying. Rejected actions leave the working document unchanged. Restore earlier missing array positions before later ones. YAML reference targets must remain valid: revert dependent references first if removing an anchor would leave them dangling. Values that cannot be represented in the current format are rejected. YAML changes patch the affected value or containing collection; reference/anchor updates can require a wider source patch.

The displayed list is limited to 200 changes, with an explicit notice when truncated; counts and tree indicators cover the complete comparison. Traversal, key identities, ancestor marking and scalar processing share a bounded work budget. Numeric operands are limited to 4,096 bytes each; a work/size failure returns an error rather than an incomplete diff. Comparison files use the same configured byte/node limits and depth limit as working files. One extra baseline is retained per in-memory session, survives working edits, and resets when a new working document opens. It follows normal session expiry/eviction and is not part of Undo/Redo.

### YAML preservation

Working YAML is stored as original bytes plus validated local patches. Value edits replace that value's source span; structural edits may rewrite the containing collection. **Bytes outside that span stay identical**, including line endings. Unchanged subtree applies preserve the entire source. Metadata on unaffected nodes is retained. Structural serialization can change spacing within the patched collection.

Structured scalar edits retain applicable comments, anchors and quoting. JSON subtree replacement that would discard YAML metadata requires explicit confirmation through the API; the UI's native YAML editor is the default for YAML. Document conversion provides the preview/confirmation flow for deliberately removing metadata.

Anchor names in duplicated content are made unique; internal references follow their cloned anchors. External references stay external. Deleting a referenced anchor is rejected. JSON conversion expands acyclic aliases and merge keys, converts tagged scalar values to JSON strings when necessary, and removes YAML-only metadata. Cycles, non-string keys and nonfinite values are rejected. Expansion work and output size are bounded.

CSV, JSON and JSONL tree edits regenerate that format's working text. Their original whitespace is available in the Original tab.

## Limits and storage

- UTF-8 input; defaults of 20 MiB and 1,000,000 parsed nodes, configurable with `-max-file-size-mb` and `-max-nodes`. Maximum nesting depth remains 256.
- Rendering is limited to 1,000 visible cards, with viewport culling, collapsed branches and pagination. Large imports are parsed server-side; the whole expanded tree is never sent to the browser.
- File browsing examines at most 10,000 directory entries and displays at most 1,000 matching folders/files, with a notice when limited. Enter an exact file path for entries outside the displayed list.
- Up to 30 history revisions within a 64 MiB source-history budget. Oldest revisions are evicted when the budget is reached.
- Browser sessions use HttpOnly, SameSite cookies. State is in memory, expires after 30 idle minutes, and disappears when the process stops. At most 16 sessions are retained; the least recently used is evicted when necessary. Tabs in the same browser session share a document; revision checks reject stale edits.
- No authentication, collaboration, persistent session storage or direct file overwrite. Explicit local-path opening reads regular files on the server computer, subject to the same format and size limits. Conversion can explicitly save a new copy beside a locally opened file; it never replaces existing files. Requests require a loopback host and state changes require the same Origin. Assets are local and CSP blocks third-party connections.

The node limit is checked while reading JSON and after YAML parsing; YAML's parser can temporarily allocate more than the final node budget. File sizes use the configured limit (20 MiB by default). Import parsing is synchronous and sessions share a server lock; this first version targets one local user.

## Verify

```sh
go test ./...
```

```sh
go test -race ./...
```

```sh
go vet ./...
```

```sh
npm test
```

Node.js 18+ is required only for the JavaScript tests; no npm packages or build step are required. Tests cover parser/format rules, source preservation including Unicode/CRLF/block scalars, reference cloning, explicit conversion, bounded merges, revision/history behavior, HTTP isolation/origin checks, retained invalid drafts, and layout/viewport hit detection. A representative near-10-MiB JSON fixture is generated in a test.

Live browser verification could not run in the implementation sandbox: local listening sockets and browser startup were blocked. The automated HTTP tests use Go's in-process recorder. Manual acceptance: load the example, pan/zoom and select a field; edit, duplicate, undo/redo and download; try YAML containing comments and aliases; verify the three resizable panes and depth-column layout at the target screen size.

## Structure

- `internal/document`: ordered AST, parsing, spans, edits, conversion and bounded history.
- `internal/server`: loopback/session checks, HTTP handlers, embedded templates and frontend.
- `cmd/datatree`: CLI, server timeouts and graceful shutdown.
- `docs/implementation.md`: implementation decisions and verification boundaries.

Third-party notices are included under `vendor/` and `internal/server/web/vendor/`.
