# Public repository preparation

## Local preparation

- Public quick start uses checked-in samples and documents binary builds.
- Contribution guidance, security reporting policy and third-party notices are included.
- CI has read-only repository permissions and commit-pinned GitHub Actions. It checks Go 1.23 and stable, race detection on stable, vet, builds, formatting, frontend tests, generated asset consistency and Chromium workflows for JSON, JSONL, YAML and CSV.
- Dependabot is configured for Go, npm and GitHub Actions. Go dependency changes must also regenerate `vendor/`.
- Ignore rules cover local builds, caches, dependency installs, browser output, environment files, private key files and editor metadata. Ignoring files does not remove previously committed content.

## Pre-upload review

1. The project uses MIT (`LICENSE`); third-party licenses remain intact. Confirm the right to publish all original source and samples.
2. Review `git status --short`, the staged diff, and the files selected for upload. Use synthetic samples; do not add local documents or browser artifacts. Commit preparation changes using a Conventional Commit message.
3. Review Git author/committer names and email addresses before publishing existing history. History was preserved; no identities or commits were rewritten.
4. Run a dedicated secret scanner against all history before upload, and inspect any findings privately. A local pattern scan of 470 historical blobs found no matches for PEM private keys, GitHub tokens, AWS access-key IDs, OpenAI-style keys or absolute personal home/temp paths. This limited scan does not detect every secret or establish that data is suitable for publication.
5. Run the checks in `CONTRIBUTING.md`. Local results are recorded in the preparation handoff; GitHub-hosted CI needs its first run after upload.

## On GitHub

Create an empty repository without generated README/license files when pushing this existing history. Add its actual remote URL locally, then push the chosen branch. No remote URL, owner or repository name is assumed here.

Enable private vulnerability reporting before relying on the Security policy's private reporting route. Review repository security settings, secret scanning/push protection, dependency alerts and branch protection as available. Require the CI checks after the first successful run.

Review third-party distribution requirements separately before publishing binary releases, particularly the bundled p5.js library. No release, tag, push or GitHub repository creation is part of this preparation.

## Local verification on 2026-10-28

Passed with Go 1.27.0 and Node.js 24.22.1:

- `go test ./...`, `go test -race ./...`, `go vet ./...` and binary build.
- `npm test`: 110 passing tests.
- `npm run build:web`: generated assets unchanged.
- Chromium with a generated 1 MiB JSON fixture: all five tests passed. The server required execution outside the restricted sandbox to bind its loopback port.
- YAML syntax, local Markdown links, ignore behavior, action commit-pin checks, Go formatting and `git diff --check`.

The Go 1.23 and Linux CI matrix, the other browser fixture formats, dedicated secret scanning and binary-release licensing qualification were not executed in this preparation. No remote was configured, and no commit, push or publication was performed.
