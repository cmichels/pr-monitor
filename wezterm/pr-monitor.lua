-- pr-monitor wezterm integration
-- Add to your wezterm.lua: require("pr-monitor")
-- Or copy the relevant sections into your existing config.

local wezterm = require("wezterm")
local mux = wezterm.mux

local M = {}

-- Status bar badge: shows pending review count in the right-status area.
-- Reads ~/.config/pr-monitor/status.json written by the pr-monitor process.
function M.setup_status_bar(config)
  wezterm.on("update-right-status", function(window, pane)
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
      return
    end

    local ok, json = pcall(wezterm.json_parse, content)
    if not ok or not json then
      return
    end

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

    if #parts > 0 then
      window:set_right_status(wezterm.format(parts))
    else
      window:set_right_status("")
    end
  end)
end

-- Auto-launch: spawns pr-monitor in a dedicated tab on wezterm startup.
function M.setup_auto_launch(config)
  wezterm.on("gui-startup", function(cmd)
    local tab, pane, window = mux.spawn_window(cmd or {})
    -- Spawn pr-monitor in a second tab
    window:spawn_tab({ args = { "pr-monitor" } })
    -- Switch back to the first tab
    tab:activate()
  end)
end

-- Convenience: set up both status bar and auto-launch.
function M.setup(config)
  M.setup_status_bar(config)
  M.setup_auto_launch(config)
end

return M
