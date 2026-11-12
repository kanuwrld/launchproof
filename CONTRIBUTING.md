# Contributing

Small, focused contributions are welcome. Open an issue before changing the project scope or adding a dependency.

## Local workflow

1. Install Go 1.26.7 or newer.
2. Run `make build` and `make check`.
3. Exercise the interactive workspace and the relevant plain/JSON commands.
4. Keep changes focused and describe the behavior and checks in your pull request.

Use Conventional Commits, for example `fix(inspect): handle conflicting lockfiles`.

## Product boundaries

Inspection, doctor, and docs validation must stay read-only. Only explicit checks may execute a validated `launchproof.json`; never execute Markdown instructions. Never collect environment values or introduce network calls into inspection. Bound and redact execution logs, retain truthful failures and skipped checks, and invalidate evidence when source changes.

Keep command output honest: a discovered script is not a passing check, and an absent document is not proof that the application is broken.

Prefer the existing libraries. Keep the business logic independent of the TUI. Preserve non-interactive JSON output and accessible plain-text operation.

## UI changes

Check wide, square, portrait, and short terminals, long project names, live resizing, keyboard navigation, `--no-color`, and `--no-motion`. Keep the footer at the bottom and ensure long content can be scrolled to the end. Regenerate the demonstration and responsive screenshots with `make demo` when the visible interface changes.

## Release packaging

From a clean committed checkout, run `python3 scripts/package-release.py 0.3.0 --output dist/0.3.0`. It cross-compiles six archives, includes documentation and dependency licenses, and writes `checksums.txt`. Use a new version and fresh output directory for each release. Publish only the commit that passed CI.
