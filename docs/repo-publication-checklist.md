# Repo Publication Checklist

Reusable process for preparing a repository for public release and portfolio visibility.

## 1) Baseline Snapshot

- Confirm branch status is clean.
- Capture remotes, default branch, visibility, description, topics, license.
- Record open security/automation settings.

## 2) Sensitive Data Audit

- Scan tracked files for credentials, tokens, private keys, and `.env` leakage.
- Scan for internal/customer identifiers that should be redacted.
- Review recent commit history for accidental leaks.

## 3) Public Surface Curation

- Ensure a high-quality `README.md` exists.
- Add `LICENSE`.
- Remove or untrack internal process folders not useful for external audiences.
- Preserve architecture context in `docs/architecture.md`.

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

## 6) Final Publish Gate

- Re-run sensitive scan on current HEAD.
- Confirm no internal identifiers remain in tracked files.
- Verify docs are complete and setup instructions work.
- Flip visibility to public only after all checks pass.

## 7) Post-Publish

- Pin repo if strategically relevant.
- Add screenshots/demo assets.
- Track follow-up improvements in a lightweight roadmap.
