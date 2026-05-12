# Repo Publication Checklist

Reusable process for preparing a repository for public release and portfolio visibility.

## Operating Rules

- Use explicit approval gates: no edits until findings are reviewed and approved.
- Apply changes in small batches; commit and push each approved batch.
- Keep this process interactive and decision-driven, not one-pass automation.

## 1) Baseline Snapshot

- Confirm branch status is clean.
- Capture remotes, default branch, visibility, description, topics, license.
- Record open security/automation settings.

## 2) Sensitive Data Audit

- Scan tracked files for credentials, tokens, private keys, and `.env` leakage.
- Scan for internal/customer identifiers that should be redacted.
- Review recent commit history for accidental leaks.
- Validate account-context behavior for integrations (for example, `gh auth` and Jira CLI auth) so runtime identity is clear and not hardcoded.

## 3) Public Surface Curation

- Ensure a high-quality `README.md` exists.
- Add `LICENSE`.
- Remove or untrack internal process folders not useful for external audiences.
- Preserve architecture context in `docs/architecture.md`.
- Ensure README reflects current runtime reality (for example, tmux-first vs optional terminal integrations).

### Keep Local but Untrack from Git

When internal folders should remain locally but not publicly visible:

1. Add paths to `.gitignore`
2. Run `git rm -r --cached <paths>`
3. Commit the untracking change

Typical candidates:

- `.claude/`
- internal planning journals or loop frameworks
- private follow-up notes and internal retrospectives

## 4) Governance and Safety

- Add `SECURITY.md`, `CONTRIBUTING.md`, and `CODEOWNERS`.
- Add issue templates and PR template.
- Add CI workflow with least-privilege permissions.

## 5) GitHub Settings

- Set accurate description and topics.
- Enable Dependabot alerts and security updates.
- Enable secret scanning once public.
- Configure branch protection (required reviews + required checks).
- Restrict GitHub Actions policy as needed.
- Confirm default workflow token permissions are read-only unless elevated scope is required.
- Verify branch-protection feature availability before planning required checks (some settings may require public visibility or plan tier).

## 6) Final Publish Gate

- Re-run sensitive scan on current HEAD.
- Confirm no internal identifiers remain in tracked files.
- Verify docs are complete and setup instructions work.
- Flip visibility to public only after all checks pass.
- Run explicit tracked-file string checks for employer identifiers and internal domains.

## 7) Post-Publish

- Pin repo if strategically relevant.
- Add screenshots/demo assets.
- Track follow-up improvements in a lightweight roadmap.

## Lessons Applied from pr-monitor

- Treat repo positioning as part of career branding, not just code hygiene.
- Prioritize a strong “Why This Exists” section tied to real engineering workflow impact.
- Keep optional integrations documented as optional; highlight the true primary workflow.
- Remove employer-identifying examples from code comments/tests and docs, even when non-sensitive.
