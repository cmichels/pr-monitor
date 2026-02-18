#!/bin/bash
# .ralph/run-all.sh — Run all Ralph phases sequentially
#
# Usage: .ralph/run-all.sh [start_phase]
#
# Arguments:
#   start_phase - (Optional) Phase number to start from (default: 1)
#
# Stops on first failure (phase exits non-zero when a task hits max iterations).

set -euo pipefail

START_PHASE="${1:-1}"

for phase in .ralph/phase*.md; do
  # Extract phase number from filename (e.g., phase3.md -> 3)
  phase_num=$(echo "$phase" | sed 's/.*phase\([0-9]*\)\.md/\1/')

  if [ "$phase_num" -lt "$START_PHASE" ]; then
    echo "--- Skipping $phase (before phase $START_PHASE) ---"
    continue
  fi

  echo ""
  echo "############################################"
  echo "  Starting: $phase"
  echo "############################################"
  .ralph/loop.sh "$phase"
done

echo ""
echo "=== All phases complete ==="
