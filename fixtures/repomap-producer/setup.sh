#!/usr/bin/env bash
# Builds repomap and uir, turns the shop demo into a clean git checkout, and imports it into a fresh
# SQLite index through the `repomap uir` producer plugin. Run from the uir repository root.
set -euo pipefail

UIR_ROOT=$(pwd -P)
REPOMAP_DIR=$(cd "${REPOMAP_DIR:-$UIR_ROOT/../repomap}" && pwd -P)
WORK="$UIR_ROOT/.tmp/repomap-producer"

rm -rf "$WORK"
mkdir -p "$WORK"
make -C "$REPOMAP_DIR" build >&2
make build >&2

cp -R fixtures/repomap-producer/shop "$WORK/shop"
git -C "$WORK/shop" init -q -b main
git -C "$WORK/shop" add -A
git -C "$WORK/shop" -c user.name=fixture -c user.email=fixture@example.com commit -qm "shop demo"

.bin/uir --dsn "$WORK/index.db" import --plugin repomap --plugin-path "$REPOMAP_DIR/.bin/repomap" "$WORK/shop" --format json >"$WORK/import.json"