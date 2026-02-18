#!/bin/bash
# .ralph/build-project.sh — Build pr-monitor from scratch
#
# Single command to go from empty project to fully built application.
# Runs Phase 0 (bootstrap) then all Ralph phases (1-7).
#
# Usage:
#   .ralph/build-project.sh              # Full build from scratch
#   .ralph/build-project.sh 3            # Resume from phase 3
#   .ralph/build-project.sh --skip-bootstrap 3  # Skip bootstrap, start at phase 3
#
# Requires: go, jq, claude, gh (authenticated)

set -euo pipefail

SKIP_BOOTSTRAP=false
START_PHASE=1

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-bootstrap)
      SKIP_BOOTSTRAP=true
      shift
      ;;
    *)
      START_PHASE="$1"
      shift
      ;;
  esac
done

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_ROOT"

echo "╔══════════════════════════════════════════════╗"
echo "║        pr-monitor — Ralph Build              ║"
echo "║                                              ║"
echo "║  Bootstrap: $([ "$SKIP_BOOTSTRAP" = true ] && echo "skip" || echo "yes ")                            ║"
echo "║  Start phase: $START_PHASE                              ║"
echo "╚══════════════════════════════════════════════╝"
echo ""

# ── Preflight checks ────────────────────────────────────────────
echo "=== Preflight checks ==="

check_cmd() {
  if command -v "$1" &>/dev/null; then
    echo "  ✓ $1"
  else
    echo "  ✗ $1 — not found. $2"
    exit 1
  fi
}

check_cmd go "Install from https://go.dev/dl/"
check_cmd jq "Install with: brew install jq"
check_cmd claude "Install Claude Code CLI"
check_cmd gh "Install with: brew install gh"

# Verify gh is authenticated
if gh auth status &>/dev/null 2>&1; then
  echo "  ✓ gh authenticated"
else
  echo "  ✗ gh not authenticated. Run: gh auth login"
  exit 1
fi

echo ""

# ── Phase 0: Bootstrap ──────────────────────────────────────────
if [ "$SKIP_BOOTSTRAP" = false ]; then
  .ralph/bootstrap.sh
  echo ""
fi

# ── Phases 1-7: Ralph loops ─────────────────────────────────────
echo "=== Starting Ralph loops from phase $START_PHASE ==="
echo ""

.ralph/run-all.sh "$START_PHASE"

echo ""
echo "╔══════════════════════════════════════════════╗"
echo "║        pr-monitor — BUILD COMPLETE           ║"
echo "║                                              ║"
echo "║  Install: go install ./cmd/pr-monitor        ║"
echo "║  Run:     pr-monitor                         ║"
echo "╚══════════════════════════════════════════════╝"
