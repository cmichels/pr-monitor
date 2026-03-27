# Session Dump — 2026-03-25 — SQLITE_BUSY Fix

## Branch: `dev`

Committed fix, remaining work still uncommitted from prior session.

```
go build -o /dev/null ./... && go vet ./... && go test ./...
```

---

## What Was Done This Session

### 1. Reviewed prior session dump
Read `follow-up/2026-03-24-settings-tab-and-hardening.md`. Validated 6 changes from that session are still uncommitted on `dev`.

### 2. Fixed SQLITE_BUSY errors (`internal/store/store.go`)
**Symptom:** Immediate `database is locked (5) (SQLITE_BUSY)` errors on startup for both Jira upserts and PR upserts — multiple goroutines hitting the DB concurrently.

**Root cause:** `database/sql` opens a connection pool. `PRAGMA busy_timeout=5000` was set on one connection, but new pool connections got default timeout (0ms) — instant failure on lock contention. Even with WAL mode, SQLite only allows one writer at a time.

**Fix:** Added `db.SetMaxOpenConns(1)` after PRAGMA setup. Serializes all DB access through a single connection. PRAGMAs persist, no lock contention, zero throughput impact for a TUI app.

**Committed:** `f3eb786 Fix SQLITE_BUSY errors by limiting connection pool to single conn`

---

## Committed Changes

| Commit | Description |
|--------|-------------|
| `f3eb786` | `SetMaxOpenConns(1)` in `store.New()` — fixes SQLITE_BUSY |

## Still Uncommitted (from 2026-03-24 session)

~335 insertions across 11 modified + 2 new files:

1. Board ID cache (`internal/jira/jira.go`)
2. Generation counters — staleness guard for all 4 data sources
3. Epic tab: exclude Done tasks (JQL filter)
4. Settings tab (tab 6) — full implementation with tests
5. Review skill: `/review-pr` → `/review-pr-team`

See `follow-up/2026-03-24-settings-tab-and-hardening.md` for full details.

---

## Key Files

| File | Change |
|------|--------|
| `internal/store/store.go` | `db.SetMaxOpenConns(1)` after PRAGMA setup (committed) |

## What's NOT Done / Known Issues

1. **Prior session's 5 features still uncommitted** — see 2026-03-24 dump
2. **Epic + Sprint tabs still not tested live** against real Jira data
3. **No "already scanning" guard** on Settings rescan
4. **Stats tab still has no spinner glyph in header**

## To Resume

1. `cd /home/kuda/projects/personal/pr-monitor`
2. `go build -o /dev/null ./... && go vet ./... && go test ./...`
3. `git diff --stat` — prior session's uncommitted work (~335 lines)
4. Consider committing the remaining changes from 2026-03-24
5. Live test Epic + Sprint tabs against real Jira data
