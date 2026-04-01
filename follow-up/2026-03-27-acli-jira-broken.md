# 2026-03-27 — acli Jira Integration Broken After WSL Reboot

## What Happened

WSL2 crashed/rebooted, wiping session state. On restart, `acli jira` commands all fail with auth errors, then after re-auth, fail with `failed to search work items`.

## Investigation Summary

1. **Initial symptom**: All pr-monitor Jira tabs erroring with `unauthorized: use 'acli [product] auth login' to authenticate`
2. **Re-auth via `acli jira auth logout && acli jira auth login`** — login succeeds ("Welcome, Chris Michels") but all search commands still fail
3. **Upgraded acli from 1.3.14 to 1.3.15** — no change, same `failed to search work items` error
4. **`acli jira auth status`** reports authenticated (controlfreak.atlassian.net, oauth) but API calls fail immediately
5. **Atlassian MCP works perfectly** — same JQL queries succeed via MCP, proving the Jira API and credentials are fine
6. **Conclusion**: acli CLI itself is broken. The bug existed before the WSL reboot/upgrade — not a version regression.

## Current State

- **pr-monitor**: All non-Jira tabs working. Jira tabs (Jira, Epics, Sprint, Stats) are broken due to acli dependency.
- **acli**: 1.3.15-stable installed, auth says OK, all `workitem search` commands fail. Bug is in acli, not in Jira API.
- **Atlassian MCP**: Fully functional. Can query Jira via MCP without issues.
- **All Ralph phases**: COMPLETE (phases 1-7). All plans up to date.
- **Git state**: Clean, on `dev`, up to date with remote. Last commit `916682a` ("add docs").

## Follow-Up Actions

- [ ] **Replace acli with direct REST API calls** in `internal/jira/jira.go` — eliminate CLI dependency entirely. The Atlassian REST API works (proven via MCP). Use `net/http` + API token or OAuth. This is the right long-term fix.
- [ ] Optionally file a bug with Atlassian for acli `workitem search` returning `failed to search work items` despite valid auth.
- [ ] After Jira backend swap, remove `acli_path` from config and `internal/jira/` CLI shelling.

## Key Files

- `internal/jira/jira.go` — acli client, all `run()` calls shell out to acli binary
- `internal/jira/types.go` — Jira data types (these stay the same regardless of backend)
- `~/.config/pr-monitor/config.yaml` — has `jira.base_url: https://controlfreak.atlassian.net`
- `~/.config/acli/jira_config.yaml` — acli auth config (cloud_id: `7d1d0780-63ed-4375-90d5-5424cc8695a3`)

## Context for Resumption

No in-progress work. Pick up from the follow-up action list above when ready.
