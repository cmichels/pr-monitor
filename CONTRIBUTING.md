# Contributing

Thanks for your interest in contributing to `pr-monitor`.

## Development Setup

1. Install Go 1.25+
2. Clone the repository
3. Build and test:

```bash
go build -o /dev/null ./... && go vet ./... && go test ./...
```

## Pull Requests

- Keep PRs focused and scoped to a single concern.
- Include tests for behavior changes.
- Update docs when user-facing behavior changes.
- Explain tradeoffs for architecture-impacting changes.

## Style

- Prefer clear, maintainable code over cleverness.
- Preserve module boundaries (`poller`, `store`, `tui`, `notify`, `discover`, `config`).
- Keep runtime behavior observable and debuggable.

## Security

- Do not commit credentials, tokens, or private keys.
- Use local `gh` authentication flows for GitHub API access.
- Report vulnerabilities via the process in `SECURITY.md`.
