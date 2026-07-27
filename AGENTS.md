# Repository Guidelines

## Project Structure & Module Organization

`acn` is a Go 1.23 command-line tool that sends Claude Code and Codex completion events to Feishu. The executable entry point and command handlers live in `cmd/acn/`. Reusable implementation packages are under `internal/`: `hook` parses Stop payloads, `claude` and `codex` inspect transcripts, `event` normalizes notifications, `feishu` performs delivery, `config` manages local settings, and `install` updates CLI configuration. Tests sit beside their packages as `*_test.go`. Build output goes to `bin/acn`; do not commit generated binaries. User documentation belongs in `README.md`.

## Build, Test, and Development Commands

- `make build` compiles `./cmd/acn` to `bin/acn` and injects the Git-derived version.
- `make test` runs `go vet ./...` followed by `go test -race ./...`; use this before opening a PR.
- `make fmt` applies `gofmt` across the repository.
- `make install` installs the current binary into `GOBIN` for local integration testing.
- `go test ./internal/hook -run TestReadStop` runs a focused test during development.

## Coding Style & Naming Conventions

Use standard Go formatting and tabs as emitted by `gofmt`. Package names are short, lowercase nouns; exported identifiers use `CamelCase`, unexported identifiers use `camelCase`, and sentinel constants use descriptive Go names rather than all caps. Keep packages narrowly scoped and wrap errors with operation context using `%w`. Existing comments and user-facing messages are primarily Chinese; preserve the surrounding language and terminology when editing them.

## Testing Guidelines

Use Go's `testing` package, `httptest` for HTTP behavior, and `t.TempDir()` for filesystem isolation. Name tests `TestFunctionBehavior`, and prefer table-driven cases for multiple related inputs. Cover success, malformed input, external-service errors, permissions, and configuration edge cases. There is no numeric coverage threshold, but changed behavior should have regression coverage and must pass the race detector.

## Commit & Pull Request Guidelines

Follow the existing Conventional Commit pattern: `feat: ...`, `fix: ...`, `refactor: ...`, or `chore: ...`; recent subjects use concise Chinese descriptions. Keep each commit focused. PRs should explain behavior changes, list verification commands, link relevant issues, and include terminal output when CLI behavior changes. Screenshots are unnecessary unless visual evidence is genuinely relevant.

## Security & Hook Constraints

Never commit Feishu webhook URLs or signing secrets. Use `ACN_FEISHU_WEBHOOK_URL`, `ACN_FEISHU_SECRET`, `ACN_DEVICE_NAME`, or a local `~/.acn/config.json`. The `acn hook` path must never write to stdout: Codex interprets hook output as control data. Send all diagnostics to stderr and keep notification failures from interrupting the parent CLI workflow.
