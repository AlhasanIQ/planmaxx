# Release

Tags matching `v*` trigger GitHub Actions to build and publish self-contained
archives for Linux, macOS, and Windows on amd64 and arm64. Releases also include
`SKILL.md`, checksums, and GitHub's tagged source archive.

Before tagging, move the completed changelog entries out of `Unreleased`, keep
the working tree clean, and run the same release gates used by GitHub Actions:

```bash
./scripts/build-web.sh
cd web && bun test && bunx tsc --noEmit
cd ..
go test ./...
go mod verify
go vet ./...
./scripts/e2e-smoke.sh
./scripts/e2e-browser.sh
bash -n install.sh
```

Create an annotated semantic-version tag on the verified commit, then push the
commit and tag:

```bash
version=v0.7.0
git tag -a "$version" -m "Release PlanMaxx $version"
git push origin main
git push origin "$version"
run_id=$(gh run list --workflow Release --branch "$version" --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$run_id" --exit-status
gh release view "$version"
```

Never move a published release tag. If publication fails, fix the workflow and
manually run **Release** with the same tag; it rebuilds that tag.

After publication, install the release and verify the packaged binary:

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
