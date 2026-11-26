#!/usr/bin/env bash
# Runs one collector twice and fails if the second run changed the archive.
#
#   client/app/tests/collectors/check-idempotency.sh <platform> [database]
#
# The database defaults to $ATRACE_DATABASE (without a "sqlite:" prefix) or
# ./data/atrace.db. Needs ego-browser, sqlite3 and a running atrace server. A
# difference can also mean the platform really changed between the two runs.
# CHECK_GAP=<seconds> waits between the runs, for platforms that rate-limit
# back-to-back requests (Zhihu answers 403 otherwise).
set -euo pipefail

platform=${1:?usage: client/app/tests/collectors/check-idempotency.sh <platform> [database]}
database=${2:-${ATRACE_DATABASE:-./data/atrace.db}}
database=${database#sqlite:}
here="$(cd "$(dirname "$0")" && pwd)"
[ -f "$here/../../../cli/collector-examples/$platform/index.mjs" ] || { echo "no collector for $platform" >&2; exit 2; }

counts() {
  sqlite3 "$database" "SELECT (SELECT count(*) FROM object_version) || ' versions, ' || (SELECT count(*) FROM activity) || ' activities'"
}

# Runs the collector, prints its summary line, and stops the check when it
# fails: a failed run proves nothing about idempotency. ego writes console
# output to stderr and ends it with a blank line, so the summary is the last
# non-empty line.
run() {
  local label=$1 output status=0
  output=$(ATRACE_COLLECTORS="$here/../../../cli/collector-examples" bun "$here/../../../cli/atrace" collect "$platform" 2>&1) || status=$?
  if [ "$status" -ne 0 ]; then
    echo "FAIL $platform: the $label run exited with $status" >&2
    echo "$output" | grep -v '^$' | tail -n 4 >&2
    exit 1
  fi
  echo "$label run: $(echo "$output" | grep -v '^$' | tail -n 1)"
}

run first
before=$(counts)
sleep "${CHECK_GAP:-0}"
run second
after=$(counts)

if [ "$before" != "$after" ]; then
  echo "FAIL $platform: the second run changed the archive ($before -> $after)" >&2
  exit 1
fi
echo "OK $platform: the second run added nothing ($after)"
