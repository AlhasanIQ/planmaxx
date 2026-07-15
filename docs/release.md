# Release

Tags matching `v*` trigger GitHub Actions to build and publish self-contained
archives for Linux, macOS, and Windows on amd64 and arm64. Releases also include
`SKILL.md`, checksums, and GitHub's tagged source archive.

```bash
git tag v0.1.0
git push origin v0.1.0
```

After publication, verify one archive:

```bash
planmaxx version
planmaxx review --no-browser path/to/plan.md
```

Release assets use `planmaxx_<version>_<os>_<arch>.tar.gz`. Generated UI files
are embedded in binaries and are not committed.

`planmaxx update` depends on the semantic-version tag, archive naming pattern,
and `checksums.txt` asset. It selects the archive for the current OS and
architecture and verifies its checksum before replacing the executable. Keep
all three stable for every release.

To rebuild from source:

```bash
cd web && bun install
cd ..
./scripts/build-web.sh
go build ./cmd/planmaxx
```

If a release fails, fix the workflow and manually run **Release** with the same
tag; it rebuilds that tag.
