# Launchproof

Go CLI using Cobra, Fang, Bubble Tea v2, and Lip Gloss v2.

- Commands live in `internal/cli`; inspection in `internal/project`; interactive state in `internal/tui`; shared rendering in `internal/ui`.
- Keep inspection, doctor, and documentation validation local and read-only. Only the explicit `check` workflow may execute commands from a validated `launchproof.json`; never interpret Markdown as executable instructions.
- Do not read secret files or export secret values. Inspect scripts by name, not command body. Bound and redact check logs, and keep generated run records private by default.
- Preserve the JSON contract and truthful distinction between discovery and verification.
- Keep TUI rendering responsive and keyboard-accessible. Preserve plain, no-color, and no-motion modes.
- Use Go formatting and focused changes. Add tests for behavior and security boundaries.
- Run `make check` and `make build` after code changes.
- Use English Conventional Commits.
