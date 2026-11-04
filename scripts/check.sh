#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
echo "== go files =="
find cmd internal -name '*.go' -print
echo "== web files =="
find web -maxdepth 2 -type f -not -path '*/node_modules/*' | sort
echo "== docs =="
find docs -type f | sort
