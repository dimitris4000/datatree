# Data Tree

A local Go web application for exploring and editing JSON, JSONL, YAML and CSV as a left-to-right tree. Each depth shares a column. Object cards group scalar fields, while nested collections branch into separate cards.

## Run

Requires Go 1.23 or newer. Go dependencies and htmx 1.9.10 are vendored.

```sh
go run ./cmd/datatree
```

Open **http://127.0.0.1:8080**. Use **Load example**, **Open file**, or paste source text and choose **Apply source**. An alternative port is available with `-listen 127.0.0.1:8090`. Listening on non-loopback addresses is rejected.

### Enable p5.js

The project uses a p5.js canvas when p5 is available, with a native Canvas 2D fallback for an offline checkout. This development environment could not download p5.js, so its distribution is **not included**. To install the pinned p5.js 1.11.11 distribution:

```sh
./scripts/vendor-p5.sh
```

Restart/rebuild the Go server afterward: frontend assets are embedded at build time. The script downloads only the library and license from public distribution URLs; application files are never sent externally. Once vendored, the entire app runs without network access. The same layout, drawing, hit detection and interactions serve both rendering backends. Real p5/browser integration still needs verification in a browser-enabled environment.

## Use

The compact top toolbar combines file opening, document identity and editing controls. Status and error messages appear in the bottom footer.

The toolbar has one **Open file** button. Its dialog offers a file picker or a local path. Enter an absolute path or a path relative to the server's working directory to open a local file with Git context. Tracked files show the current branch and short HEAD commit beside the filename; hover for the full hash. Detached checkouts show **Detached HEAD**. Git details refresh with state responses (including page reloads and edits), and describe the repository checkout, not a commit of your in-memory edits. Untracked files, files outside a repository, unborn repositories and systems without Git show no Git badge. The dialog’s file picker hides full paths, so picker uploads cannot supply Git context. All opening methods keep edits in memory and use **Download** to save a copy.

- **Collapse all** in the tree controls collapses the selected node and every descendant, including hidden branches and other pages. Other branches retain their expansion state. With no selection, it collapses the entire tree.
- **Source** on the left contains editable working text, line numbers, a line/column indicator and a read-only Original tab. Selecting a tree card or field highlights and scrolls to its source; moving the editor cursor or selecting text selects the smallest enclosing node in the tree and inspector, opening ancestors and loading the required pages. Selection stays in the editor while the inspector updates. Sync pauses for unapplied source drafts and for Original text that differs from the working document; apply or discard the draft to resume. Apply explicitly; invalid source stays in the editor and the tree retains its last valid state.
- **Tree** supports drag-to-pan, wheel/button zoom, fit, and expand/collapse using the triangle on a collection card. Double-click a collection title to collapse it when expanded, or open its next level when collapsed. Select a collection to show the level input and **Expand X** button in the bottom controls; X levels are counted beneath that node, and existing expanded branches stay open. Expansion uses each collection’s current page (20 children) and stops after 1,000 collections per action. Collections show 20 children per page. Use the footer to page through fields or items. The keyboard node list below the canvas provides selection and expansion buttons; pane dividers also accept arrow keys.
- Right-click a card or field for **Duplicate**, **Rename**, and **Delete**. Object entries ask for a new key; array items duplicate directly. Delete requires confirmation and can be undone. Actions that do not apply to a node are disabled. Escape or clicking outside closes the menu. From the keyboard node list, press **Shift+F10** (or the menu key), use arrow keys to choose an action, and press Enter.
- Click a card header to select its subtree, or an object field to select that value. The **Inspector** shows its location, native subtree editor, JSON/YAML representations, structured value editing and collection insertion. Duplicate within the same parent; object duplicates require a unique key. Use the triangles inside JSON/YAML representations to collapse nested blocks, or **Collapse all** / **Expand all**. In Subtree, choose **Foldable view** to browse nested blocks and **Edit text** to return to editing. Folding never changes the full text used by Copy or Apply. Copy text or a location using the Copy buttons.
- Apply or discard source drafts before mutations. Pending subtree edits prompt before selection changes. Invalid subtree input remains editable. All accepted changes are reversible with Undo/Redo.
- **Convert…** opens a preview; **Confirm conversion** updates the working source. JSON-to-YAML conversion uses block mappings, dash-prefixed lists and two-space indentation, matching the Inspector’s YAML representation. Strings are quoted only when needed, and numeric precision and key order are retained. **Download** saves the working file as a copy. Undo restores the original format and source; the app never overwrites the uploaded file.

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

### YAML preservation

Working YAML is stored as original bytes plus validated local patches. Value edits replace that value's source span; structural edits may rewrite the containing collection. **Bytes outside that span stay identical**, including line endings. Unchanged subtree applies preserve the entire source. Metadata on unaffected nodes is retained. Structural serialization can change spacing within the patched collection.

Structured scalar edits retain applicable comments, anchors and quoting. JSON subtree replacement that would discard YAML metadata requires explicit confirmation through the API; the UI's native YAML editor is the default for YAML. Document conversion provides the preview/confirmation flow for deliberately removing metadata.

Anchor names in duplicated content are made unique; internal references follow their cloned anchors. External references stay external. Deleting a referenced anchor is rejected. JSON conversion expands acyclic aliases and merge keys, converts tagged scalar values to JSON strings when necessary, and removes YAML-only metadata. Cycles, non-string keys and nonfinite values are rejected. Expansion work and output size are bounded.

CSV, JSON and JSONL tree edits regenerate that format's working text. Their original whitespace is available in the Original tab.

## Limits and storage

- UTF-8 input; maximum 10 MiB, 100,000 parsed nodes and nesting depth 256.
- Rendering is limited to 1,000 visible cards, with viewport culling, collapsed branches and pagination. Large imports are parsed server-side; the whole expanded tree is never sent to the browser.
- Up to 30 history revisions within a 64 MiB source-history budget. Oldest revisions are evicted when the budget is reached.
- Browser sessions use HttpOnly, SameSite cookies. State is in memory, expires after 30 idle minutes, and disappears when the process stops. At most 16 sessions are retained; the least recently used is evicted when necessary. Tabs in the same browser session share a document; revision checks reject stale edits.
- No authentication, collaboration, persistent disk storage or direct file overwrite. Explicit local-path opening reads regular files on the server computer, subject to the same format and size limits. Requests require a loopback host and state changes require the same Origin. Assets are local and CSP blocks third-party connections.

The node limit is checked while reading JSON and after YAML parsing; YAML's parser can temporarily allocate more than the final node budget. All file sizes remain limited to 10 MiB. Import parsing is synchronous and sessions share a server lock; this first version targets one local user.

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
