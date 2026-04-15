# Releasing `go-hydaelyn`

`go-hydaelyn` is a Go module. Publishing is controlled by git tags, not by uploading package artifacts to a separate registry.

## What counts as a release

- A pushed semver tag such as `v0.1.0`, `v0.2.3`, or `v1.0.0-rc.1`
- The corresponding commit must already be on `main`
- Consumers install with:

```bash
go get github.com/Viking602/go-hydaelyn@v0.1.0
```

## GitHub Actions behavior

- `.github/workflows/ci.yml`
  - runs on pushes to `main`
  - runs on pull requests
  - verifies the module path
  - runs `go test ./...`
- `.github/workflows/release.yml`
  - runs only on pushed tags matching `v*`
  - validates the tag is semver
  - validates Go module major-version rules
  - runs `go test ./...`
  - creates a GitHub Release for that tag

## Release steps

1. Make sure `main` contains the release commit.
2. Run local verification:

```bash
go test ./...
```

3. Create and push the release tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

4. Wait for the `release` GitHub Action to finish.
5. Confirm the GitHub Release exists and `go list -m github.com/Viking602/go-hydaelyn@v0.1.0` resolves.

## Versioning rules

- Use semver tags prefixed with `v`
- `v0.x.y` is fine while the API is still moving
- `v1.x.y` is the first stable major
- If you ever release `v2.0.0` or later, update `go.mod` to use a major-version suffix:

```go
module github.com/Viking602/go-hydaelyn/v2
```

and update all internal imports to match that suffix before tagging `v2.0.0`
