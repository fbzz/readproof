#!/usr/bin/env bash
#
# The whole support-agent story in one command.
#
#   bash scripts/scenario.sh                      # needs Ollama with a chat model
#   SUPPORT_FAKE_MODEL=1 bash scripts/scenario.sh # no Ollama, no network
#   OLLAMA_MODEL=llama3.2 bash scripts/scenario.sh
#
# Self-contained: builds readproof and readproofd from this repo, runs a
# throwaway readproofd on :18090 with its own data directory, and restores
# the two policy fixtures it edits — always, including on failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$EXAMPLE_DIR/../.." && pwd)"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/readproof-support-agent.XXXXXX")"
READPROOFD_ADDR=":18090"
READPROOFD_URL="http://localhost:18090"
READPROOFD_PID=""

REFUNDS="$EXAMPLE_DIR/context/policies/refunds.md"
TONE="$EXAMPLE_DIR/context/policies/tone.md"
QUESTION="I bought headphones 20 days ago. Can I still get a refund?"

# Fixtures are backed up by copy rather than restored with git, so an
# interrupted run cannot leave the working tree dirty and cannot clobber
# edits someone had in flight.
cp "$REFUNDS" "$TMP/refunds.md.orig"
cp "$TONE" "$TMP/tone.md.orig"

cleanup() {
  local status=$?
  if [ -n "$READPROOFD_PID" ] && kill -0 "$READPROOFD_PID" 2>/dev/null; then
    kill "$READPROOFD_PID" 2>/dev/null || true
    wait "$READPROOFD_PID" 2>/dev/null || true
  fi
  cp "$TMP/refunds.md.orig" "$REFUNDS"
  cp "$TMP/tone.md.orig" "$TONE"
  echo ""
  echo "fixtures restored; scratch dir left at $TMP"
  exit $status
}
trap cleanup EXIT INT TERM

step() {
  echo ""
  echo "=============================================================="
  echo "  $*"
  echo "=============================================================="
}

agent() {
  ( cd "$EXAMPLE_DIR" && node dist/src/cli.js "$@" )
}

step "0. Build readproof, readproofd, the SDK and the example"

( cd "$REPO_ROOT" && go build -o "$TMP/readproof" ./cmd/readproof )
( cd "$REPO_ROOT" && go build -o "$TMP/readproofd" ./cmd/readproofd )
echo "built $TMP/readproof and $TMP/readproofd"

if [ ! -d "$REPO_ROOT/sdk/typescript/dist" ]; then
  echo "building @readproof/sdk (the example consumes it as a file: dependency)"
  ( cd "$REPO_ROOT/sdk/typescript" && npm ci --silent && npm run build --silent )
fi

if [ ! -d "$EXAMPLE_DIR/node_modules" ]; then
  ( cd "$EXAMPLE_DIR" && npm ci --silent )
fi
( cd "$EXAMPLE_DIR" && npm run build --silent )
echo "example built"

step "1. Start a throwaway readproofd on $READPROOFD_URL"

"$TMP/readproofd" --addr "$READPROOFD_ADDR" --data-dir "$TMP/data" >"$TMP/readproofd.log" 2>&1 &
READPROOFD_PID=$!

for _ in $(seq 1 50); do
  if curl -sf "$READPROOFD_URL/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done
if ! curl -sf "$READPROOFD_URL/healthz" >/dev/null 2>&1; then
  echo "readproofd did not become healthy; log follows:" >&2
  cat "$TMP/readproofd.log" >&2
  exit 1
fi
echo "readproofd is up (pid $READPROOFD_PID, data dir $TMP/data)"

# Everything below talks to that readproofd, and keeps its ticket log out of the
# repo so repeat runs start from a clean slate.
export READPROOF_ENDPOINT="$READPROOFD_URL"
export SUPPORT_DATA_DIR="$TMP/agent-data"

if [ "${SUPPORT_FAKE_MODEL:-}" = "1" ]; then
  echo "model: deterministic fake (SUPPORT_FAKE_MODEL=1) — no Ollama needed"
else
  echo "model: Ollama at ${OLLAMA_HOST:-http://localhost:11434}${OLLAMA_MODEL:+ (OLLAMA_MODEL=$OLLAMA_MODEL)}"
fi

step "2. setup — register the three policies, tag tone@prod"
agent setup

step "3. ticket 1001 — asked while the policy says 30 days"
agent ask 1001 "$QUESTION"

step "4. Someone edits the refund policy: 30 days -> 14 days"
sed 's/within 30 days/within 14 days/' "$REFUNDS" >"$TMP/refunds.new"
mv "$TMP/refunds.new" "$REFUNDS"
grep -n "refunded within" "$REFUNDS"

step "5. ticket 1002 — same question, after the edit"
agent ask 1002 "$QUESTION"

step "6. diff 1001 1002 — which document moved, and why"
agent diff 1001 1002

step "7. replay 1001 — the old answer's exact bytes, from the manifest"
agent replay 1001

step "8. evidence 1001 — an in-toto bundle for the auditor"
agent evidence 1001 --out "$TMP/ticket-1001.bundle.json" --with-content

step "9. Verify that bundle with the Go CLI, as an auditor would"
"$TMP/readproof" --server "$READPROOFD_URL" evidence verify "$TMP/ticket-1001.bundle.json"

step "10. Someone edits the house style — but nobody promoted it"
printf '\nAlways open with a one-line summary of the decision.\n' >>"$TONE"
tail -n 2 "$TONE"

step "11. ticket 1003 — refunds moved, tone did NOT (it is pinned @prod)"
agent ask 1003 "$QUESTION"

step "12. show 1003 — the tone entry is still the promoted snapshot"
agent show 1003

step "Done"
echo "ticket 1003's tone entry has the same snapshot id as 1001's: editing"
echo "tone.md changed nothing, because the agent mounts tone@prod. Moving"
echo "the tag is a deliberate act:  npm run agent -- promote tone"
