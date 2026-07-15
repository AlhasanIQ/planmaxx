# Contributing

PlanMaxx uses Go for the CLI/server and React for the review UI.

```bash
cd web && bun install
cd ..
./scripts/build-web.sh
go test ./...
go vet ./...
cd web && bun test && bunx tsc --noEmit
```

Keep generated `internal/review/static/`, autosaves, and environment files out
of commits. Keep prompts in `internal/prompts/templates/`.
