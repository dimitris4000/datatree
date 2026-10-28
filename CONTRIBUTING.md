# Contributing

Data Tree is a local Go application with embedded browser assets. Use Go 1.23+ and a Node.js version supported by `package.json`; Node.js 24.15+ in the 24.x line is recommended.

## Development

```sh
npm ci
go run ./cmd/datatree samples/projects.json
```

Edit the source modules under `internal/server/web/`, then run `npm run build:web` and restart the Go server. Commit the regenerated `app.bundle.js`, its linked legal comments and `vendor/editor.LICENSE` with source changes. Do not edit generated assets directly. Go dependencies are vendored; dependency updates must also update `go.sum` and `vendor/`.

## Before opening a pull request

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o bin/datatree ./cmd/datatree
npm test
npm run build:web
git diff --check
npx playwright install chromium
DATATREE_BROWSER_MIB=1 npm run test:browser
```

The browser command uses generated JSON data. Repeat with `DATATREE_BROWSER_FORMAT=jsonl`, `yaml` and `csv` for format changes. See [large-file performance checks](docs/large-files-performance.md) for opt-in larger fixtures. Report automated checks and browser/manual verification separately, including any environment limitations.

Add regression coverage for behavior changes, including source preservation, format constraints and stale revisions where relevant. Format Go with `gofmt` and match surrounding JavaScript style. Preserve loopback-only serving, same-origin mutation checks, file overwrite protections, document limits and offline schema resolution.

Keep pull requests focused. Explain the problem, resulting behavior and validation; include screenshots for visual changes. Use Conventional Commits, for example `fix(parser): reject duplicate keys`.

Use synthetic fixtures. Never include private documents, credentials, browser traces containing user data, or machine-specific paths in issues or commits. See [SECURITY.md](SECURITY.md) for vulnerability reporting.
