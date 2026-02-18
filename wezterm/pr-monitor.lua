-- pr-monitor wezterm integration
-- Works standalone OR alongside bar.wezterm.
-- See README.md for setup instructions.

local wezterm = require("wezterm")
local mux = wezterm.mux

local M = {}

-- Read and parse the status JSON file.
-- Returns the parsed table or nil on any error.
local function read_status()
  local home = os.getenv("HOME") or ""
  local status_path = home .. "/.config/pr-monitor/status.json"

  local success, content = pcall(function()
    local f = io.open(status_path, "r")
    if not f then return nil end
    local data = f:read("*a")
    f:close()
    return data
  end)

  if not success or not content or content == "" then
    return nil
  end

  local ok, json = pcall(wezterm.json_parse, content)
  if not ok or not json then
    return nil
  end

  return json
end

-- Returns wezterm.format()-compatible elements for the PR badge.
-- Use this to integrate with bar.wezterm or any custom status bar.
--
-- Usage in wezterm.lua:
--   local pr = require("pr-monitor")
--   local badge = pr.get_badge_text()
--   -- then prepend/append to your bar output
function M.get_badge_elements()
  local json = read_status()
  if not json then return {} end

  local parts = {}

  if json.review_count and json.review_count > 0 then
    local review_color = "#4caf50"
    if json.oldest_review_age_hours and json.oldest_review_age_hours > 48 then
      review_color = "#f44336"
    elseif json.oldest_review_age_hours and json.oldest_review_age_hours > 24 then
      review_color = "#ff9800"
    elseif json.oldest_review_age_hours and json.oldest_review_age_hours > 4 then
      review_color = "#f9a825"
    end

    table.insert(parts, { Foreground = { Color = review_color } })
    table.insert(parts, { Text = string.format(" %d review%s ", json.review_count, json.review_count == 1 and "" or "s") })
  end

  if json.authored_activity_count and json.authored_activity_count > 0 then
    table.insert(parts, { Foreground = { Color = "#64b5f6" } })
    table.insert(parts, { Text = string.format(" %d update%s ", json.authored_activity_count, json.authored_activity_count == 1 and "" or "s") })
  end

  return parts
end

-- Returns the badge as a pre-formatted string.
-- Useful for simple concatenation with other status text.
function M.get_badge_text()
  local elements = M.get_badge_elements()
  if #elements == 0 then return "" end
  return wezterm.format(elements)
end

-- Standalone status bar: sets the right-status directly.
-- Use this ONLY if you are NOT using bar.wezterm or another status bar plugin.
function M.setup_status_bar(config)
  wezterm.on("update-right-status", function(window, pane)
    local elements = M.get_badge_elements()
    if #elements > 0 then
      window:set_right_status(wezterm.format(elements))
    else
      window:set_right_status("")
    end
  end)
end

-- Auto-launch: spawns pr-monitor in a dedicated tab on wezterm startup.
function M.setup_auto_launch(config)
  wezterm.on("gui-startup", function(cmd)
    local tab, pane, window = mux.spawn_window(cmd or {})
    window:spawn_tab({ args = { "/bin/zsh", "-lc", "pr-monitor" } })
    tab:activate()
  end)
end

-- Convenience: standalone status bar + auto-launch.
-- Use setup_with_bar() instead if you use bar.wezterm.
function M.setup(config)
  M.setup_status_bar(config)
  M.setup_auto_launch(config)
end

-- Integration with bar.wezterm: no status bar badge needed.
-- The Go binary sets its own tab title (e.g. "pr-monitor | 2 reviews | 1 update")
-- which is visible in the tab bar without conflicting with bar.wezterm.
-- Launch pr-monitor via keybinding (LEADER + g) instead of gui-startup.
function M.setup_with_bar(config)
  -- No-op: auto-launch removed in favor of keybinding.
  -- Kept for API compatibility.
end

return M
