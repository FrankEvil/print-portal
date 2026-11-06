#!/bin/bash
set -euo pipefail

VERSION="${1:-0.1.0}"
VERSION="${VERSION#v}"
echo "VERSION=${VERSION}"

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "version=${VERSION}" >> "$GITHUB_OUTPUT"
fi
