# pr-monitor wezterm integration

Lua module for integrating pr-monitor with your wezterm status bar and tab layout.

## Installation

### Option 1: Symlink (recommended)

Symlink the module into your wezterm config directory so it stays in sync with updates:

```sh
ln -s "$(pwd)/wezterm/pr-monitor.lua" ~/.config/wezterm/pr-monitor.lua
```

### Option 2: Copy

```sh
cp wezterm/pr-monitor.lua ~/.config/wezterm/pr-monitor.lua
```

## Usage

Add to your `~/.config/wezterm/wezterm.lua`:

```lua
local wezterm = require("wezterm")
local pr_monitor = require("pr-monitor")

local config = wezterm.config_builder()

-- Enable both status bar badge and auto-launch tab
pr_monitor.setup(config)

-- Or enable individually:
-- pr_monitor.setup_status_bar(config)
-- pr_monitor.setup_auto_launch(config)

return config
```

## What it does

### Status bar badge

Displays pending review and authored PR activity counts in the wezterm right-status area. Reads `~/.config/pr-monitor/status.json` which the pr-monitor process writes after every poll cycle.

The badge shows:

- **Review count** — PRs waiting for your review, color-coded by age:
  - Green (`#4caf50`): less than 4 hours old
  - Yellow (`#f9a825`): 4-24 hours old
  - Orange (`#ff9800`): 24-48 hours old
  - Red (`#f44336`): older than 48 hours
- **Update count** — new activity on PRs you authored (approvals, comments, change requests), shown in blue (`#64b5f6`)

When there are no pending items, the status bar is cleared.

### Auto-launch tab

Spawns `pr-monitor` in a dedicated tab when wezterm starts, then switches focus back to your first tab. If `pr-monitor` is not in your `PATH`, update the args in `setup_auto_launch`:

```lua
window:spawn_tab({ args = { "/path/to/pr-monitor" } })
```

## Customizing colors

Edit the color values in `pr-monitor.lua` to match your terminal theme. The relevant variables are in the `setup_status_bar` function:

```lua
-- Review age thresholds and colors
local review_color = "#4caf50"   -- default (< 4h)
review_color = "#f9a825"         -- > 4 hours
review_color = "#ff9800"         -- > 24 hours
review_color = "#f44336"         -- > 48 hours

-- Authored activity color
{ Foreground = { Color = "#64b5f6" } }
```

## Status JSON format

The pr-monitor process writes this file atomically after each poll:

```json
{
  "review_count": 3,
  "authored_activity_count": 2,
  "last_updated": "2026-02-18T14:30:00Z",
  "oldest_review_age_hours": 72.5
}
```

| Field                    | Type   | Description                                    |
|--------------------------|--------|------------------------------------------------|
| `review_count`           | int    | PRs awaiting your review                       |
| `authored_activity_count`| int    | New events on PRs you authored                 |
| `last_updated`           | string | ISO 8601 timestamp of last poll                |
| `oldest_review_age_hours`| float  | Hours since the oldest pending review was filed |
