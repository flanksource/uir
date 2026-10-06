#!/usr/bin/env bash
# Benchmarks `uir reindex --all` for fixtures/bench/reindex-all.md against copies of a real uir
# database. Run from the repository root:
#
#   reindex-all.sh base      copy UIR_PROFILE_BASE (default ~/.config/uir/uir.db) to .tmp/profile/base.db
#   reindex-all.sh cold <n>  copy base.db to .tmp/profile/run-<n>.db and reindex --all it
#   reindex-all.sh noop      reindex --all .tmp/profile/run-1.db a second time
#
# The base database is only ever read: every write lands on a copy under .tmp/profile. Each reindex
# writes CPU and heap profiles and an execution trace beside its database (run-<n>.cpu.pprof,
# run-<n>.mem.pprof, run-<n>.trace, and run-1-noop.* for the second pass), its uir output to
# <name>.json, and prints one JSON object with the row counts before and after, their delta and the
# wall time. Under `gavel fixtures --profile` the CPU and heap profiles are also copied to the paths
# gavel declares in GAVEL_CPU_PROFILE and GAVEL_MEM_PROFILE, so they land in its benchmark report.
set -euo pipefail

fail() {
  echo "$*" >&2
  exit 1
}

test -f fixtures/bench/reindex-all.sh || fail "run from the uir repository root, not $(pwd)"
test -n "${EPOCHREALTIME:-}" || fail "bash $BASH_VERSION has no EPOCHREALTIME: run with bash 5 or newer"
dir=.tmp/profile
mkdir -p "$dir"
# .tmp may itself be a symlink to a cache volume; only .tmp/profile escaping it is refused.
tmp_dir=$(cd .tmp && pwd -P)
profile_dir=$(cd "$dir" && pwd -P)
case "$profile_dir/" in
  "$tmp_dir"/*) ;;
  *) fail "refusing: $dir resolves to $profile_dir, which is not under .tmp ($tmp_dir)" ;;
esac

uir_bin=${UIR_BIN:-.bin/uir}
test -x "$uir_bin" || fail "$uir_bin is not an executable: run make binary or set UIR_BIN"

# Tables whose row counts every run reports; a missing one fails the count.
counted_tables=(snapshots documents source_deltas symbol_deltas package_coverage locations location_heads)
# Tables a migration may add: reported as null while absent.
optional_tables=(snapshot_stats)

# query runs one statement against a database copy with query_only set, so counting never changes the
# copy a run is about to measure. A uir database is in WAL mode, which sqlite3 -readonly cannot open
# without an existing -shm file, and an immutable open would ignore a -wal file uir left behind.
query() {
  sqlite3 -cmd 'PRAGMA query_only = 1' "$@"
}

# counts prints the row counts of a database as one JSON object.
counts() {
  local db=$1 select="" table present
  for table in "${counted_tables[@]}"; do
    select+="(SELECT count(*) FROM $table) AS $table, "
  done
  for table in "${optional_tables[@]}"; do
    present=$(query "$db" "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = '$table'")
    if [ "$present" = 1 ]; then
      select+="(SELECT count(*) FROM $table) AS $table, "
    else
      select+="NULL AS $table, "
    fi
  done
  select+="(SELECT count(*) FROM locations l LEFT JOIN location_heads h ON h.location_id = l.id"
  select+=" WHERE h.location_id IS NULL AND l.kind <> 'external') AS missing_heads"
  query -json "$db" "SELECT $select" | jq '.[0]'
}

# remove_database deletes a database copy and its journal files, refusing anything outside .tmp/profile.
remove_database() {
  case "$1" in
    "$dir"/*.db) rm -f "$1" "$1-wal" "$1-shm" "$1-journal" ;;
    *) fail "refusing to remove $1: not a .db file under $dir" ;;
  esac
}

# require_quiet_base fails while the base database has uncheckpointed WAL writes, which a plain file
# copy would silently drop.
require_quiet_base() {
  if [ -s "$1-wal" ]; then
    fail "$1-wal is not empty: a uir process has pending writes; stop it (or checkpoint) before copying"
  fi
}

copy_base() {
  local base=${UIR_PROFILE_BASE:-$HOME/.config/uir/uir.db} base_dir base_counts
  test -f "$base" || fail "base database $base does not exist: set UIR_PROFILE_BASE"
  base_dir=$(cd "$(dirname "$base")" && pwd -P)
  case "$base_dir/" in
    "$profile_dir"/*) fail "refusing: base database $base lies under $dir, which this benchmark overwrites" ;;
  esac
  require_quiet_base "$base"
  remove_database "$dir/base.db"
  cp "$base" "$dir/base.db"
  require_quiet_base "$base"
  cmp -s "$base" "$dir/base.db" || fail "$base changed while it was copied: stop every uir process and rerun"
  base_counts=$(counts "$dir/base.db")
  jq -n --arg base "$base" --arg copy "$dir/base.db" --argjson bytes "$(wc -c <"$dir/base.db")" \
    --argjson counts "$base_counts" '{base: $base, copy: $copy, bytes: $bytes, counts: $counts}'
}

# publish_profile copies a profile to the destination gavel declares under --profile.
publish_profile() {
  local source=$1 destination=$2
  if [ -n "$destination" ]; then
    cp "$source" "$destination"
  fi
}

# reindex_all runs `uir reindex --all` on db with profiles named after name and prints the result.
reindex_all() {
  local case_name=$1 db=$2 name=$3 before after started finished kind
  test -f "$db" || fail "$db does not exist"
  for kind in cpu.pprof mem.pprof trace json; do
    rm -f "$dir/$name.$kind"
  done
  before=$(counts "$db")
  started=$EPOCHREALTIME
  env -u UIR_DSN -u UIR_SCHEMA -u GOWORK "$uir_bin" --format json --dsn "$db" reindex --all \
    --cpuprofile "$dir/$name.cpu.pprof" --memprofile "$dir/$name.mem.pprof" --trace "$dir/$name.trace" \
    >"$dir/$name.json"
  finished=$EPOCHREALTIME
  for kind in cpu.pprof mem.pprof trace; do
    test -s "$dir/$name.$kind" || fail "uir wrote no $kind to $dir/$name.$kind"
  done
  publish_profile "$dir/$name.cpu.pprof" "${GAVEL_CPU_PROFILE:-}"
  publish_profile "$dir/$name.mem.pprof" "${GAVEL_MEM_PROFILE:-}"
  after=$(counts "$db")
  jq -n --arg case "$case_name" --arg db "$db" --arg prefix "$dir/$name" \
    --argjson wall_ms "$(((${finished/[.,]/} - ${started/[.,]/}) / 1000))" \
    --argjson before "$before" --argjson after "$after" --slurpfile result "$dir/$name.json" '
    {
      case: $case,
      db: $db,
      wall_ms: $wall_ms,
      profiles: {cpu: ($prefix + ".cpu.pprof"), mem: ($prefix + ".mem.pprof"), trace: ($prefix + ".trace")},
      before: $before,
      after: $after,
      delta: ($before | with_entries(.value = if .value == null or $after[.key] == null then null else $after[.key] - .value end)),
      result: $result[0]
    }'
}

case "${1:-}" in
  base)
    copy_base
    ;;
  cold)
    n=${2:-}
    case "$n" in
      '' | *[!0-9]* | 0) fail "cold needs a positive run number, got '$n'" ;;
    esac
    test -f "$dir/base.db" || fail "$dir/base.db does not exist: run $0 base first"
    remove_database "$dir/run-$n.db"
    cp "$dir/base.db" "$dir/run-$n.db"
    reindex_all cold-all "$dir/run-$n.db" "run-$n"
    ;;
  noop)
    test -f "$dir/run-1.db" || fail "$dir/run-1.db does not exist: run $0 cold 1 first"
    reindex_all noop-all "$dir/run-1.db" run-1-noop
    ;;
  *)
    fail "usage: $0 base | cold <n> | noop"
    ;;
esac
