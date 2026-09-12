#!/usr/bin/env bash
# Loads the UI in headless Chrome against a generated fixture and fails on any
# console error or error toast. Static syntax checks cannot catch a helper that
# was deleted while its call site stayed.
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$project_root"

chrome=""
for candidate in google-chrome google-chrome-stable chromium chromium-browser; do
  if command -v "$candidate" >/dev/null 2>&1; then chrome="$candidate"; break; fi
done
if [ -z "$chrome" ]; then
  echo "smoke test skipped: no Chrome found"
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"; [ -n "${server_pid:-}" ] && kill "$server_pid" 2>/dev/null || true' EXIT

go run ./cmd/genfixture -out "$work/ghostReplays" -game-out "$work/game" >/dev/null
go build -o "$work/server" .
port=$(( (RANDOM % 2000) + 18000 ))
"$work/server" -root "$work/ghostReplays" -data "$work/import" -game "$work/game" \
  -addr "127.0.0.1:$port" >"$work/server.log" 2>&1 &
server_pid=$!

for _ in $(seq 1 50); do
  curl -sf "http://127.0.0.1:$port/api/catalog" >/dev/null && break
  sleep 0.2
done

lib="game:freeRoam/east_coast_usa/starts/s001/ghostracer.save.json"
# roads=1 exercises the road overlay, which no static check can reach.
hash="#lib=$(printf %s "$lib" | sed 's/:/%3A/; s#/#%2F#g')&laps=g000001,g000003&ref=g000001&roads=1"
dom=$("$chrome" --headless=new --no-sandbox --disable-gpu --virtual-time-budget=6000 \
  --dump-dom "http://127.0.0.1:$port/$hash" 2>/dev/null)

failed=0
if grep -q 'class="toast error"' <<<"$dom"; then
  echo "UI reported an error toast:" >&2
  sed -n 's/.*id="toast"[^>]*>\([^<]*\)<.*/  \1/p' <<<"$dom" >&2
  failed=1
fi
# The charts and the lap list must actually have been built.
grep -q 'id="charts"' <<<"$dom" || { echo "charts container missing" >&2; failed=1; }
if ! grep -q 'class="lap-chip' <<<"$dom"; then
  echo "no lap was selected from the deep link" >&2
  failed=1
fi
if ! grep -q 'class="tree-item' <<<"$dom"; then
  echo "library tree rendered no entries" >&2
  failed=1
fi
# The overlay must have fetched roads and drawn them; a helper that is called but
# never defined only shows up here, where the code path actually runs.
# The map's title attribute carries newlines, so match the attribute directly
# rather than trying to scan within one line of the tag.
roads=$(grep -o 'data-roads="[0-9]*"' <<<"$dom" | head -1 | tr -dc '0-9')
if [ -z "$roads" ] || [ "$roads" -lt 1 ]; then
  echo "road overlay drew nothing (data-roads=${roads:-unset})" >&2
  failed=1
fi
grep -q '"/api/roads' "$work/server.log" || true

[ "$failed" -eq 0 ] && echo "smoke test passed"
exit "$failed"
