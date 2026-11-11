# Contributing

Small, focused contributions are welcome. Open an issue before changing the project scope or adding a dependency.

## Local workflow

1. Install Go 1.26.7 or newer.
2. Run `make build` and `make check`.
3. Exercise both the interactive screen and `inspect --json`.
4. Keep changes focused and describe the behavior and checks in your pull request.

Use Conventional Commits, for example `fix(inspect): handle conflicting lockfiles`.

## Product boundaries

Inspection must stay read-only. Do not execute commands discovered in manifests or Markdown. Never collect environment values or introduce network calls into inspection.

Keep command output honest: a discovered script is not a passing check, and an absent document is not proof that the application is broken.

Prefer the existing libraries. Keep the business logic independent of the TUI. Preserve non-interactive JSON output and accessible plain-text operation.

## UI changes

Check narrow and large terminals, long project names, keyboard navigation, `--no-color`, and `--no-motion`. Regenerate the demonstration with `make demo` when the visible interface changes.
