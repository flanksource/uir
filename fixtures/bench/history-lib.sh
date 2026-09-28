# shellcheck shell=bash
# Sourced from the repository root by every block of fixtures/bench/history.md and by history.sh.
# It refuses to run without HISTORY_DSN, so an unset variable never indexes into the query corpus or
# ~/.config/uir/uir.db, and passes the DSN as --dsn, which every uir build accepts.
set -euo pipefail

if [ -z "${HISTORY_DSN:-}" ]; then
  echo 'HISTORY_DSN is unset: write .tmp/uir-history.env as fixtures/bench/history.md describes' >&2
  exit 78
fi

HISTORY_BIN=${UIR_BIN:-.bin/uir}
export HISTORY_ROOT_KEY=github.com/flanksource/commons
export GOWORK=off

# uir runs the selected binary against HISTORY_DSN (and HISTORY_SCHEMA when set), ignoring any
# UIR_DSN or UIR_SCHEMA the environment carries for the query corpus.
uir() {
  local schema=()
  if [ -n "${HISTORY_SCHEMA:-}" ]; then
    schema=(--schema "$HISTORY_SCHEMA")
  fi
  env -u UIR_DSN -u UIR_SCHEMA "$HISTORY_BIN" "$@" --dsn "$HISTORY_DSN" ${schema[@]+"${schema[@]}"}
}
