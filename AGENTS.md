# Repository Guidelines

## Project Structure & Module Organization

Data Tree is a local Go web app for exploring and editing JSON, JSONL, YAML, and CSV.

- `cmd/datatree/`: CLI entry point and startup tests.
- `internal/document/`: parsing, editing, source mapping, queries, diff/revert, and schema validation.
- `internal/compact/` and `internal/textstore/`: compact document records and shared source storage.
- `internal/server/`: HTTP handlers, sessions, local file access, and embedded frontend assets in `web/`.
- `tests/browser/`: Playwright integration tests; `internal/perftest/`: performance fixtures.
- `samples/`: example documents with companion `*.schema.json` files.
- `docs/`: implementation notes, designs, plans, and performance reports.

## Build, Test, and Development Commands

Use Go 1.23+ and a Node version supported by `package.json`.

- `go run ./cmd/datatree`: serve on a random four-digit loopback port (up to five attempts); open the printed URL.
- `go run ./cmd/datatree -listen 127.0.0.1:8090 samples/projects.json`: open a sample on another port.
- `go build ./cmd/datatree`: compile the executable.
- `npm ci`: install locked frontend dependencies.
- `npm run build:web`: regenerate the checked-in JavaScript bundle and dependency license notices. Rebuild/restart Go afterward because assets are embedded.
- `go test ./...`, `go test -race ./...`, `go vet ./...`: run Go tests, race checks, and static analysis.
- `npm test`: run Node frontend tests.
- `npm run test:browser`: run Playwright Chromium integration tests; install its browser first if unavailable.

## Coding Style & Naming Conventions

Format Go with `gofmt`; use standard Go naming and colocated `*_test.go` files. JavaScript uses ES modules, camelCase identifiers, and colocated `*.test.js` files. Match surrounding indentation and formatting; no dedicated JavaScript lint command is configured. Edit frontend source modules, then regenerate `app.bundle.js` rather than editing it directly.

## Testing Guidelines

Use Go's testing package, Node's built-in test runner, and Playwright (`*.spec.mjs`). Add regression tests for behavior changes, including format constraints, source preservation, and stale revisions where relevant. No numeric coverage threshold is configured. Report automated checks and browser verification separately.

## Commit & Pull Request Guidelines

Every future commit message must follow Conventional Commits: `<type>[optional scope][!]: <description>`. Use `feat` for new functionality, `fix` for bug fixes, and appropriate types such as `docs`, `test`, `refactor`, `perf`, `build`, `ci`, or `chore` for other changes. Write concise imperative descriptions, for example `fix(parser): reject duplicate keys`. Mark breaking changes with `!` before the colon or a `BREAKING CHANGE:` footer.

Keep commits focused. Describe behavior changes, related issues, and validation in pull requests; include screenshots for visual changes. Preserve unrelated working changes.

## Security & Configuration

Keep the server loopback-only and preserve same-origin mutation checks. File operations use the server process's filesystem permissions. Retain document limits, offline schema resolution, and protection against overwriting existing files.
