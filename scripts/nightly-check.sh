#!/usr/bin/env bash
# The nightly self-check: the honest first slice of
# self-correction. Every night it runs the whole proof
# stack against the current build and writes a dated
# report: the Go suite (logic), the five simulators
# (behaviour without a model), the live reasoning eval
# (the real model, the real key, synthetic data only), and
# the deployed service's health. A green night says so in
# one line; a red one names the layer and keeps its first
# failure lines. The key is fetched from the server for
# this run only: never printed, never written to disk.
set -u
cd "$(dirname "$0")/.."
export PATH="$HOME/workspace/tools/go/bin:$PATH"

REPORT_DIR="${NIGHTLY_REPORT_DIR:-$HOME/workspace/goals/meta-hackathon-whatsapp-sales-agent-entry/hidden_files/nightly}"
mkdir -p "$REPORT_DIR"
STAMP="$(date -u +%Y-%m-%d)"
REPORT="$REPORT_DIR/$STAMP.md"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

overall=0
{
  echo "# Nightly self-check, $STAMP (UTC)"
  echo

  echo "## Go suite"
  if go test ./... -count=1 > "$WORK/suite.log" 2>&1; then
    echo "PASS: $(grep -c '^ok' "$WORK/suite.log") packages ok, 0 FAIL."
  else
    echo "FAIL:"
    grep -E '^--- FAIL' "$WORK/suite.log" | head -10
    overall=1
  fi
  echo

  echo "## Simulators"
  go build -o agent ./cmd/agent
  for s in simulate simulate-model simulate-onboarding simulate-two-shops simulate-photo; do
    if node "scripts/$s.js" > "$WORK/$s.log" 2>&1; then
      echo "PASS: $s"
    else
      echo "FAIL: $s"
      grep -E 'FAIL:' "$WORK/$s.log" | head -5
      overall=1
    fi
  done
  echo

  echo "## Live reasoning eval"
  KEY="$(ssh -i /home/hatch/.ssh/id_ed25519 -o UserKnownHostsFile=/home/hatch/.ssh/known_hosts -o ConnectTimeout=15 root@84.247.138.190 "grep '^META_MODEL_API_KEY=' /home/valorm/workspace/apps/hackathon-agent/app.env | cut -d= -f2-" 2>/dev/null)"
  if [ -z "$KEY" ]; then
    echo "UNREACHABLE: the key could not be fetched from the server, so the live layer did not run."
    overall=1
  elif META_MODEL_API_KEY="$KEY" node scripts/eval-reasoning.js > "$WORK/eval.log" 2>&1; then
    echo "PASS: $(grep -c '^PASS' "$WORK/eval.log") of 7 reasoning cases, live model."
  else
    echo "FAIL:"
    grep -E '^FAIL' "$WORK/eval.log" | head -7
    overall=1
  fi
  unset KEY
  echo

  echo "## Deployed service"
  if curl -s -m 15 https://agent.valormtech.site/healthz | grep -q '"ok":true'; then
    echo "PASS: https://agent.valormtech.site/healthz answers ok."
  else
    echo "FAIL: the deployed service did not answer ok."
    overall=1
  fi
  echo

  if [ "$overall" -eq 0 ]; then
    echo "RESULT: GREEN"
  else
    echo "RESULT: RED"
  fi
} > "$REPORT" 2>&1

cat "$REPORT"
exit "$overall"
