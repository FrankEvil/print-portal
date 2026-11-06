#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="${VERSION:-0.1.0}"
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

mkdir -p "$WORK_DIR/docker" "$WORK_DIR/ui"
cp "$SCRIPT_DIR/../../../apps/print-portal/fnos/docker/docker-compose.yaml" "$WORK_DIR/docker/"
sed -i.bak "s/\${VERSION}/${VERSION}/g" "$WORK_DIR/docker/docker-compose.yaml"
rm -f "$WORK_DIR/docker/docker-compose.yaml.bak"
cp -a "$SCRIPT_DIR/../../../apps/print-portal/fnos/ui/." "$WORK_DIR/ui/"

cd "$WORK_DIR"
tar czf "$SCRIPT_DIR/../../../app.tgz" docker ui/
echo "Built app.tgz for Print Portal ${VERSION}"
