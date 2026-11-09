#!/bin/bash
# Build a fnOS .fpk from the app definition in deployments/fnos-app.
#
# The fpk is a tar archive that contains the app definition plus app.tgz, whose
# MD5 is recorded in the manifest checksum field. Packaging by hand keeps the
# release reproducible on a machine that has no fnOS toolchain.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-}"
PLATFORM="${PLATFORM:-x86}"

if [ -z "$VERSION" ]; then
  VERSION="$(sed -n 's/^version[[:space:]]*=[[:space:]]*//p' \
    "$ROOT/deployments/fnos-app/print-portal/fnos/manifest" | head -1 | tr -d '[:space:]')"
fi
if [ -z "$VERSION" ]; then
  echo "cannot determine version" >&2
  exit 1
fi

APP_DIR="$ROOT/deployments/fnos-app/print-portal/fnos"
OUT_DIR="$ROOT/dist"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$OUT_DIR"

# app.tgz holds the compose file and the web UI definition.
mkdir -p "$WORK/app/docker" "$WORK/app/ui"
sed "s/\${VERSION}/${VERSION}/g" "$APP_DIR/docker/docker-compose.yaml" > "$WORK/app/docker/docker-compose.yaml"
cp -a "$APP_DIR/ui/." "$WORK/app/ui/"
tar -czf "$WORK/app.tgz" -C "$WORK/app" docker ui

CHECKSUM="$(md5 -q "$WORK/app.tgz" 2>/dev/null || md5sum "$WORK/app.tgz" | cut -d' ' -f1)"

# Stage the fpk layout, mirroring the official package structure.
STAGE="$WORK/fpk"
mkdir -p "$STAGE"
cp "$WORK/app.tgz" "$STAGE/app.tgz"
cp -a "$APP_DIR/cmd" "$STAGE/cmd"
cp -a "$APP_DIR/config" "$STAGE/config"
cp -a "$APP_DIR/ui" "$STAGE/ui"
cp -a "$APP_DIR/wizard" "$STAGE/wizard" 2>/dev/null || true
cp "$APP_DIR/print-portal.sc" "$STAGE/print-portal.sc"
cp "$APP_DIR/ui/images/icon_256.png" "$STAGE/ICON_256.PNG"
cp "$APP_DIR/ui/images/64.png" "$STAGE/ICON.PNG"

sed -e "s/^version .*/version         = ${VERSION}/" \
    -e "s/^platform .*/platform        = ${PLATFORM}/" \
    -e "s/^checksum .*/checksum        = ${CHECKSUM}/" \
    "$APP_DIR/manifest" > "$STAGE/manifest"

OUT="$OUT_DIR/print-portal_${VERSION}_${PLATFORM}.fpk"
# fnOS expects top-level names without a "./" prefix, so list them explicitly.
(cd "$STAGE" && tar -czf "$OUT" $(ls -A))
echo "$OUT"
