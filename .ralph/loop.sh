#!/bin/bash
# .ralph/loop.sh — Ralph loop runner for phase-based task files
#
# Usage: .ralph/loop.sh .ralph/phase1.md [task_id]
#
# Reads a phase file containing JSON task definitions and markdown task
# descriptions. Runs each task through a Ralph loop until success criteria
# are met or max_iterations is reached.
#
# Arguments:
#   phase_file  - Path to the phase markdown file
#   task_id     - (Optional) Run only this task, e.g. "1.1"
#
# Requires: jq, claude (Claude Code CLI)

set -euo pipefail

PHASE_FILE="${1:?Usage: .ralph/loop.sh <phase-file> [task_id]}"
TARGET_TASK="${2:-}"
GUARDRAILS=".ralph/guardrails.md"

if [ ! -f "$PHASE_FILE" ]; then
  echo "Error: Phase file not found: $PHASE_FILE"
  exit 1
fi

if ! command -v jq &>/dev/null; then
  echo "Error: jq is required. Install with: brew install jq"
  exit 1
fi

if ! command -v claude &>/dev/null; then
  echo "Error: claude CLI is required."
  exit 1
fi

# Extract JSON block from the phase file (between ```json and ```)
TASKS_JSON=$(sed -n '/^```json$/,/^```$/p' "$PHASE_FILE" | sed '1d;$d')
NUM_TASKS=$(echo "$TASKS_JSON" | jq length)

echo "=== Phase file: $PHASE_FILE ($NUM_TASKS tasks) ==="

for task_idx in $(seq 0 $((NUM_TASKS - 1))); do
  TASK_ID=$(echo "$TASKS_JSON" | jq -r ".[$task_idx].id")
  TASK_NAME=$(echo "$TASKS_JSON" | jq -r ".[$task_idx].name")
  TEST_CMD=$(echo "$TASKS_JSON" | jq -r ".[$task_idx].test_command")
  MAX_ITER=$(echo "$TASKS_JSON" | jq -r ".[$task_idx].max_iterations")

  # Skip tasks if a specific task_id was requested
  if [ -n "$TARGET_TASK" ] && [ "$TASK_ID" != "$TARGET_TASK" ]; then
    echo "--- Skipping task $TASK_ID (target: $TARGET_TASK) ---"
    continue
  fi

  echo ""
  echo "=========================================="
  echo "  Task $TASK_ID: $TASK_NAME"
  echo "  Test: $TEST_CMD"
  echo "  Max iterations: $MAX_ITER"
  echo "=========================================="

  # Extract the task's markdown section (from ## Task X.Y: to next ## Task or EOF)
  TASK_SECTION=$(awk -v pat="## Task ${TASK_ID}:" '
    $0 ~ pat { found=1 }
    found && /^## Task / && !($0 ~ pat) { found=0 }
    found { print }
  ' "$PHASE_FILE")

  if [ -z "$TASK_SECTION" ]; then
    echo "Error: Could not extract section for task $TASK_ID from $PHASE_FILE"
    exit 1
  fi

  for i in $(seq 1 "$MAX_ITER"); do
    echo ""
    echo "--- Task $TASK_ID, iteration $i / $MAX_ITER ---"

    # Build prompt from task section + guardrails
    PROMPT="$TASK_SECTION"
    if [ -f "$GUARDRAILS" ]; then
      PROMPT="$PROMPT

---

## Guardrails (learned rules — READ THESE FIRST)

$(cat "$GUARDRAILS")"
    fi

    # Run Claude Code with the assembled prompt
    echo "$PROMPT" | claude --print --dangerously-skip-permissions

    # Check success criteria
    if eval "$TEST_CMD"; then
      echo ""
      echo "=== Task $TASK_ID: SUCCESS on iteration $i ==="
      git add -A
      git diff --cached --quiet || git commit -m "ralph: task $TASK_ID complete on iteration $i"
      break
    fi

    echo ""
    echo "=== Task $TASK_ID: iteration $i failed, rotating ==="
    git add -A
    git diff --cached --quiet || git commit -m "ralph: task $TASK_ID iteration $i progress"

    if [ "$i" -eq "$MAX_ITER" ]; then
      echo ""
      echo "=== Task $TASK_ID: MAX ITERATIONS ($MAX_ITER) REACHED ==="
      echo "=== Stopping phase execution ==="
      exit 1
    fi
  done
done

echo ""
echo "=== All tasks in $PHASE_FILE completed successfully ==="
