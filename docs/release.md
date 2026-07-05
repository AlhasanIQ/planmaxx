# Release

PlanMaxx releases are built from tags that match `v*`.

## Build Model

The release workflow:

1. installs Go and Bun,
2. builds the React UI into `internal/review/static/`,
3. runs Go and web checks,
4. cross-compiles self-contained binaries,
5. archives each binary,
6. publishes archives and `checksums.txt` to GitHub Releases.

Generated UI assets are not committed. Release binaries embed them.

## Source

Each release is tied to a version tag. The corresponding GPLv3 source is the
tagged repository source archive that GitHub publishes with the release. The
binary archives include `README.md` and `LICENSE`; rebuild from source with:

```bash
cd web && bun install
./scripts/build-web.sh
go build ./cmd/planmaxx
```

## Artifacts

Release asset names use:

```text
planmaxx_<version>_<os>_<arch>.tar.gz
```

Supported targets:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`
- `windows/arm64`

## Manual Release

```bash
git tag v0.1.0
git push origin v0.1.0
```

After the workflow finishes, download one artifact and run:

```bash
planmaxx version
planmaxx review --no-browser path/to/plan.md
```
