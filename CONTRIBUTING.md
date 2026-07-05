# Contributing

PlanMaxx is a Go CLI with a React review UI.

## Setup

```bash
cd web && bun install
./scripts/build-web.sh
```

## Checks

Run these before opening a pull request:

```bash
./scripts/build-web.sh
go test ./...
go vet ./...
cd web && bun test && bunx tsc --noEmit
./scripts/e2e-smoke.sh
```

## Notes

- Keep generated files under `internal/review/static/` out of git.
- Keep comments and prompts in the existing Go/template boundaries.
- Keep changes scoped and boring.
- Do not commit local autosave files or environment files.
