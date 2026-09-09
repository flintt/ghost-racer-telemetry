#!/usr/bin/env bash
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$project_root"

echo "== gofmt =="
unformatted=$(gofmt -l . || true)
if [ -n "$unformatted" ]; then
  echo "these files need gofmt:"
  echo "$unformatted"
  exit 1
fi

echo "== go vet =="
go vet ./...

echo "== go test =="
go test ./...

echo "== frontend syntax =="
node --check web/app.js
node -e '
  const fs = require("fs")
  const html = fs.readFileSync("web/index.html", "utf8")
  const js = fs.readFileSync("web/app.js", "utf8")
  // Every id the script reaches for must exist in the markup.
  for (const match of js.matchAll(/\bel\(.([A-Za-z0-9_]+).\)/g)) {
    if (!html.includes(`id="${match[1]}"`)) throw new Error("app.js uses missing element id: " + match[1])
  }
'

echo "== ui smoke =="
"$project_root/tools/smoke.sh"

echo "== build =="
go build -o /dev/null .
GOOS=windows GOARCH=amd64 go build -o /dev/null .

echo "all checks passed"
