#!/usr/bin/env bash
# tmux status bar integration for pr-monitor.
# Reads ~/.config/pr-monitor/status.json and outputs a formatted badge.
#
# Usage in tmux.conf:
#   set -g status-right '#(~/.config/pr-monitor/tmux-status.sh)'
#
# Or copy this script somewhere on your PATH and reference it:
#   set -g status-right '#(/path/to/pr-monitor.sh)'

STATUS_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/pr-monitor/status.json"

if [ ! -f "$STATUS_FILE" ]; then
  exit 0
fi

# Parse JSON with lightweight tools (jq preferred, python fallback).
if command -v jq >/dev/null 2>&1; then
  review_count=$(jq -r '.review_count // 0' "$STATUS_FILE" 2>/dev/null)
  activity_count=$(jq -r '.authored_activity_count // 0' "$STATUS_FILE" 2>/dev/null)
  age_hours=$(jq -r '.oldest_review_age_hours // 0' "$STATUS_FILE" 2>/dev/null)
else
  # Fallback: python (available in most WSL/macOS setups).
  read -r review_count activity_count age_hours <<< "$(python3 -c "
import json, sys
try:
    d = json.load(open('$STATUS_FILE'))
    print(d.get('review_count', 0), d.get('authored_activity_count', 0), d.get('oldest_review_age_hours', 0))
except:
    print('0 0 0')
" 2>/dev/null)"
fi

output=""

if [ "${review_count:-0}" -gt 0 ] 2>/dev/null; then
  # Color based on oldest review age.
  age_int=${age_hours%.*}  # truncate to integer
  if [ "${age_int:-0}" -gt 48 ]; then
    color="#[fg=#f44336]"  # red
  elif [ "${age_int:-0}" -gt 24 ]; then
    color="#[fg=#ff9800]"  # orange
  elif [ "${age_int:-0}" -gt 4 ]; then
    color="#[fg=#f9a825]"  # yellow
  else
    color="#[fg=#4caf50]"  # green
  fi

  suffix="reviews"
  [ "$review_count" -eq 1 ] && suffix="review"
  output="${color} ${review_count} ${suffix}#[default]"
fi

if [ "${activity_count:-0}" -gt 0 ] 2>/dev/null; then
  suffix="updates"
  [ "$activity_count" -eq 1 ] && suffix="update"
  output="${output}#[fg=#64b5f6] ${activity_count} ${suffix}#[default]"
fi

printf '%s' "$output"
