#!/usr/bin/env bash
# Cross-compiles the release binaries into dist/. The web assets are embedded,
# so each binary is self-contained: copy one file to the target machine and run.
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$project_root"

dist_dir="${DIST_DIR:-$project_root/dist}"
mkdir -p "$dist_dir"

build() {
  local goos="$1" goarch="$2" output="$3"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "$dist_dir/$output" .
  echo "  $output  $(du -h "$dist_dir/$output" | cut -f1)"
}

echo "building into $dist_dir:"
build windows amd64 ghost-racer-telemetry.exe
build linux amd64 ghost-racer-telemetry-linux-amd64

(cd "$dist_dir" && sha256sum ghost-racer-telemetry.exe ghost-racer-telemetry-linux-amd64)
