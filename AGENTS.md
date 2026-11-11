# Launchproof

Go CLI using Cobra, Fang, Bubble Tea v2, and Lip Gloss v2.

- Commands live in `internal/cli`; inspection in `internal/project`; interactive state in `internal/tui`; shared rendering in `internal/ui`.
- Keep inspection local and read-only. Never execute project scripts or interpret Markdown as executable instructions.
- Do not read or export secret values. Inspect scripts by name, not command body.
- Preserve the JSON contract and truthful distinction between discovery and verification.
- Keep TUI rendering responsive and keyboard-accessible. Preserve plain, no-color, and no-motion modes.
- Use Go formatting and focused changes. Add tests for behavior and security boundaries.
- Run `make check` and `make build` after code changes.
- Use English Conventional Commits.
