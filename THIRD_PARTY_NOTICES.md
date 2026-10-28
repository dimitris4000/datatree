# Third-party notices

Third-party components retain their own licenses and copyright notices. A project license does not replace these terms. Keep the notices with redistributed source and embedded browser assets.

| Component | Version | License / bundled notice |
| --- | --- | --- |
| jsonschema | v6.0.3 | [Apache-2.0](vendor/github.com/santhosh-tekuri/jsonschema/v6/LICENSE) |
| go.yaml.in/yaml/v3 | v3.0.4 | [MIT and Apache-2.0](vendor/go.yaml.in/yaml/v3/LICENSE); [NOTICE](vendor/go.yaml.in/yaml/v3/NOTICE) |
| golang.org/x/text | v0.14.0 | [BSD-3-Clause](vendor/golang.org/x/text/LICENSE) |
| htmx | 1.9.10 | [BSD-2-Clause](internal/server/web/vendor/htmx.LICENSE) |
| p5.js | 1.11.11 | [LGPL-2.1](internal/server/web/vendor/p5.LICENSE) |
| CodeMirror, Lezer and bundled editor dependencies | Pinned in package-lock.json | [MIT notices for each bundled package](internal/server/web/vendor/editor.LICENSE) |

The frontend build emits linked legal comments when its inputs include them. Development dependencies retain their package-level licenses in their upstream distributions.

## Sources and asset updates

- Go dependency versions and source are recorded in `go.mod`, `go.sum` and `vendor/`.
- Editor dependency versions and distribution URLs are recorded in `package-lock.json`. `npm run build:web` regenerates the bundle and its dependency notices.
- The unmodified p5.js distribution is fetched by `scripts/vendor-p5.sh`. Its upstream source is [processing/p5.js, tag v1.11.11](https://github.com/processing/p5.js/tree/v1.11.11).
- htmx upstream source is [bigskysoftware/htmx, tag v1.9.10](https://github.com/bigskysoftware/htmx/tree/v1.9.10).

Before distributing compiled binaries or modified third-party libraries, review the applicable licenses, including p5.js source and modification requirements, and include the necessary notices and source materials in the release. This repository preparation does not qualify a binary release.
