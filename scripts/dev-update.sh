#!/bin/bash
# Hot-update a running local print-portal container with the current working
# tree. Use this for local verification; do not build an fpk or push a release
# until the change has been tested.
#
# Usage:
#   CONTAINER=print-portal-local ./scripts/dev-update.sh
#   GOARCH=arm64 CONTAINER=print-portal-local ./scripts/dev-update.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTAINER="${CONTAINER:-print-portal-local}"
GOARCH="${GOARCH:-amd64}"
GO="${GO:-$ROOT/.tools/go/bin/go}"
if [ ! -x "$GO" ]; then
  GO="$(command -v go || true)"
fi
if [ -z "$GO" ]; then
  echo "go toolchain not found; set GO=/path/to/go" >&2
  exit 1
fi
# Reuse the bundled local toolchain cache when present; otherwise Go uses the
# user's normal cache.
if [ -d "$ROOT/.tools/gocache" ]; then
  export GOCACHE="$ROOT/.tools/gocache"
fi
if [ -d "$ROOT/.tools/gopath" ]; then
  export GOPATH="$ROOT/.tools/gopath"
fi
export GOFLAGS="${GOFLAGS:--mod=mod}"

echo "== build frontend =="
(cd "$ROOT/web" && pnpm build)

echo "== build printd (linux/$GOARCH) =="
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" "$GO" build -trimpath -ldflags="-s -w" \
  -o /tmp/printd-dev ./cmd/printd)

echo "== copy binary and web assets into $CONTAINER =="
docker cp /tmp/printd-dev "$CONTAINER":/usr/local/bin/printd
tar -czf /tmp/print-portal-web-dev.tgz -C "$ROOT/web/dist" .
docker cp /tmp/print-portal-web-dev.tgz "$CONTAINER":/tmp/web-dev.tgz
docker exec "$CONTAINER" sh -c 'rm -rf /opt/print-portal/web/* && mkdir -p /opt/print-portal/web && tar -xzf /tmp/web-dev.tgz -C /opt/print-portal/web'

echo "== restart =="
docker restart "$CONTAINER" >/dev/null

echo "== health =="
for i in $(seq 1 30); do
  if curl -fsS --max-time 2 "${HEALTH_URL:-http://127.0.0.1:8088/healthz}" >/dev/null 2>&1; then
    echo "printd is up; verify in the browser before packaging"
    exit 0
  fi
  sleep 1
done
echo "health check failed; run: docker logs --tail 50 $CONTAINER" >&2
exit 1
