# Releasing `venat`

`venat` is a Go module. Publishing is controlled by git tags, not by uploading package artifacts to a separate registry.

## What counts as a release

- A pushed semver tag such as `v0.1.0`, `v0.2.3`, or `v1.0.0-rc.1`
- The corresponding commit must already be on `main`
- Consumers install with:

```bash
go get github.com/Viking602/venat@v0.12.0
```

## GitHub Actions behavior

- `.github/workflows/ci.yml`
  - runs on pushes to `main`
  - runs on pull requests
  - verifies the module path
  - runs `go test ./...`
- `.github/workflows/release.yml`
  - runs only on pushed tags matching `v*`
  - uses read-only repository permissions for every check; only the final
    release job receives `contents: write`
  - pins checkout and Go setup actions to reviewed commit SHAs
  - verifies the Sentrux binary, Go plugin metadata, query, and grammar against
    fixed SHA-256 checksums before running them
  - rejects moved tags and tags whose commit is not contained in `origin/main`
  - validates the tag is semver
  - validates Go module major-version rules
  - validates release-note status and stable-release README metadata
  - runs the full test, race, static-analysis, vulnerability, architecture, and soak gates
  - installs the tagged CLI with `GOPROXY=direct` and checks its reported version and command behavior
  - builds and tests a temporary consumer module against the same tag
  - reads the remote tag again after all gates and creates a GitHub Release only
    when its final commit still matches the gated commit
  - marks prerelease tags correctly and prepends the checked-in release notes
    to GitHub's generated notes

## Release steps

1. Finalize the release documentation in the release commit:
   - for a prerelease tag, set the matching release notes status to
     `Status: **Prerelease**`
   - for a stable tag, set it to `Status: **Released**`, update the README's
     latest-release link to that tag, and remove the matching unreleased notice
2. Make sure `main` contains the release commit.
3. Run the full local CI-parity gate:

```bash
make ci-local
```

4. Confirm the release commit is contained in `origin/main`:

```bash
git fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main
git merge-base --is-ancestor HEAD origin/main
```

5. Create and push the release tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

6. Wait for the `release` GitHub Action to finish. The workflow resolves both
   lightweight and annotated tags, records the gated commit, then fetches the
   remote tag again immediately before release creation. A moved tag or a tag
   that leaves `origin/main` stops the release.
7. Confirm the installed runtime reports the pushed tag:

```bash
tmp_bin="$(mktemp -d)"
GOBIN="$tmp_bin" GOPROXY=direct \
  go install github.com/Viking602/venat/cmd/venat@v0.12.0
test "$("$tmp_bin/venat" version)" = "v0.12.0"
"$tmp_bin/venat" --help
rm -rf "$tmp_bin"
```

8. Confirm the GitHub Release exists and
   `go list -m github.com/Viking602/venat@v0.12.0` resolves.

## Updating release tooling

- Resolve action versions from the action repository's official git tags and
  keep the full 40-character commit SHA in `uses`, with the readable version in
  the adjacent comment.
- Take Sentrux asset digests from the official GitHub Release metadata and
  verify them against downloaded bytes before updating the workflow.
- `sentrux plugin add` does not accept a version. The workflow therefore installs
  the Go plugin manifest and query from the immutable Sentrux release commit and
  the Go grammar from the checksummed release archive. Update their checksums
  together in CI and release workflows.

## Versioning rules

- Use semver tags prefixed with `v`
- `v0.x.y` is fine while the API is still moving
- `v1.x.y` is the first stable major
- If you ever release `v2.0.0` or later, update `go.mod` to use a major-version suffix:

```go
module github.com/Viking602/venat/v2
```

and update all internal imports to match that suffix before tagging `v2.0.0`
