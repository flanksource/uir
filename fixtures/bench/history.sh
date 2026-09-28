#!/usr/bin/env bash
# Builds the deterministic commons history that fixtures/bench/history.md diffs. Run from the
# repository root: it copies ../commons without .git or Git-ignored files into .tmp/history/commons,
# commits it with a fixed
# author and date, indexes it, then makes and reindexes 20 scripted commits. Per-run uir output goes to
# .tmp/history/logs; the last reindex result is printed.
source fixtures/bench/history-lib.sh

root=$(pwd -P)
source_dir=../commons
test -f "$source_dir/go.mod" || { echo "$source_dir/go.mod is missing: check out github.com/flanksource/commons next to this repository" >&2; exit 1; }
mkdir -p .tmp/history
history_dir=$(cd .tmp/history && pwd -P)
case "$history_dir/" in
  "$root"/.tmp/*) ;;
  *) echo "refusing: $history_dir is not under $root/.tmp" >&2; exit 1 ;;
esac
scratch=$history_dir/commons
logs=$history_dir/logs

# A SQLite history database is recreated, and only under .tmp/; a PostgreSQL schema must be dropped
# by hand before a run.
case "$HISTORY_DSN" in
  postgres://* | postgresql://* | *=*) ;;
  *.db)
    database_dir=$(mkdir -p "$(dirname "$HISTORY_DSN")" && cd "$(dirname "$HISTORY_DSN")" && pwd -P)
    case "$database_dir/" in
      "$root"/.tmp/*) rm -f "$HISTORY_DSN" "$HISTORY_DSN-wal" "$HISTORY_DSN-shm" ;;
      *) echo "refusing: HISTORY_DSN $HISTORY_DSN is not under $root/.tmp" >&2; exit 1 ;;
    esac
    ;;
  *) echo "HISTORY_DSN must be a .db path under .tmp/ or a PostgreSQL DSN, got $HISTORY_DSN" >&2; exit 1 ;;
esac

rm -rf "$scratch" "$logs"
mkdir -p "$scratch" "$logs"
# Copy the working tree as Git sees it: tracked and untracked files, but no .git and nothing Git
# ignores. An ignored .go file (commons keeps local hack/ scripts) would be indexed but untracked, which
# marks every snapshot dirty, and uir diff only selects clean snapshots.
git -C "$source_dir" ls-files -z --cached --others --exclude-standard |
  (
    cd "$source_dir"
    while IFS= read -r -d '' path; do
      if [ -e "$path" ]; then
        printf '%s\0' "$path"
      fi
    done | tar --null -T - -cf -
  ) | tar -xf - -C "$scratch"

export GIT_AUTHOR_NAME="UIR History" GIT_AUTHOR_EMAIL="history@example.org"
export GIT_COMMITTER_NAME="UIR History" GIT_COMMITTER_EMAIL="history@example.org"
export GIT_AUTHOR_DATE="2026-01-01T00:00:00Z" GIT_COMMITTER_DATE="2026-01-01T00:00:00Z"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
git -C "$scratch" init -q -b main

# commit refuses to touch any repository but the scratch copy's own.
commit() {
  local toplevel
  toplevel=$(git -C "$scratch" rev-parse --show-toplevel)
  if [ "$toplevel" != "$scratch" ]; then
    echo "refusing: $scratch resolves to Git toplevel $toplevel" >&2
    exit 1
  fi
  git -C "$scratch" add -A .
  git -C "$scratch" commit -q -m "$1"
}

body_edit() {
  N=$1 perl -0pi -e 's/^(func NewBufferedLogger\(maxLogs int\) \*BufferedLogger \{\n)/$1\t_ = $ENV{N}\n/m or die "NewBufferedLogger not found\n"' "$scratch/logger/buffered.go"
}

signature_change() {
  perl -0pi -e 's/func \(b \*BufferedLogger\) Named\(name string\) Logger \{/func (b *BufferedLogger) Named(label string) Logger {/ or die "BufferedLogger.Named not found\n"' "$scratch/logger/buffered.go"
}

add_function() {
  printf '\n// HistoryMarker reports the scripted history commit that added it.\nfunc HistoryMarker() int { return 10 }\n' >>"$scratch/logger/buffered.go"
}

remove_function() {
  perl -0pi -e 's/\nfunc DeleteEmptyStrings\(s \[\]string\) \[\]string \{\n.*?\n\}\n//s or die "DeleteEmptyStrings not found\n"' "$scratch/collections/slice.go"
}

bump_go_sum() {
  printf 'example.org/historybench v1.0.0 h1:%s=\nexample.org/historybench v1.0.0/go.mod h1:%s=\n' \
    "$(printf 'A%.0s' $(seq 43))" "$(printf 'B%.0s' $(seq 43))" >>"$scratch/go.sum"
}

commit "commons copy"
uir --format json add "$scratch" --no-workspace-uses >"$logs/00-add.json"

for i in $(seq 1 20); do
  case $i in
    5) signature_change ;;
    10) add_function ;;
    15) remove_function ;;
    18) bump_go_sum ;;
    *) body_edit "$i" ;;
  esac
  commit "history $i"
  uir --format json reindex "$scratch" >"$logs/$(printf %02d "$i")-reindex.json"
done

test "$(git -C "$scratch" rev-list --count HEAD)" = 21
cat "$logs/20-reindex.json"
