#!/usr/bin/env bash
# Builds the release binaries into dist/. The web assets are embedded, so each
# binary is self-contained: copy one file to the target machine and run it.
#
#   ./tools/build.sh            # dev build of the host-relevant targets
#   ./tools/build.sh v0.1.0     # release build, version-stamped filenames
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$project_root"

version="${1:-dev}"
dist_dir="${DIST_DIR:-$project_root/dist}"
rm -rf "$dist_dir"
mkdir -p "$dist_dir"

targets=(
  "windows amd64 .exe"
  "linux amd64 "
  "linux arm64 "
  "darwin arm64 "
)

echo "building $version into $dist_dir:"
for target in "${targets[@]}"; do
  read -r goos goarch extension <<<"$target"
  output="ghost-racer-telemetry-$version-$goos-$goarch$extension"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w -X main.version=$version" -o "$dist_dir/$output" .
  echo "  $output  $(du -h "$dist_dir/$output" | cut -f1)"
done

(cd "$dist_dir" && sha256sum ghost-racer-telemetry-* > SHA256SUMS && cat SHA256SUMS)
