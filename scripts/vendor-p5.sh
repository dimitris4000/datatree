#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
destination=internal/server/web/vendor/p5.min.js
temporary="$destination.tmp"
license=internal/server/web/vendor/p5.LICENSE
trap 'rm -f "$temporary" "$license.tmp"' EXIT
curl --fail --location --proto '=https' --tlsv1.2 --max-time 60 https://cdn.jsdelivr.net/npm/p5@1.11.11/lib/p5.min.js -o "$temporary"
curl --fail --location --proto '=https' --tlsv1.2 --max-time 60 https://cdn.jsdelivr.net/npm/p5@1.11.11/license.txt -o "$license.tmp"
test -s "$temporary"
test -s "$license.tmp"
mv "$temporary" "$destination"
mv "$license.tmp" "$license"
printf 'Vendored p5.js 1.11.11. Rebuild/restart the Go server to embed it.\n'
