# GoLand Go Format Standard

This repository treats GoLand formatting as a command-line reproducible contract, not as a private IDE preference.

## Source Of Truth

- `.editorconfig` controls cross-file whitespace: UTF-8, LF, final newline, trailing whitespace trimming, tab indentation for Go and Makefile files.
- `.goland/inspection-profile.xml` is the tracked GoLand headless inspection profile for repository review.
- `.golangci.yml` enables `gofmt` and `goimports`, with `github.com/Viking602/go-hydaelyn` as the local import prefix.

## Go Formatting Contract

Use the same order locally and in review:

1. `$(go env GOPATH)/bin/goimports -w -local github.com/Viking602/go-hydaelyn $(git ls-files '*.go')`
2. `gofmt -l $(git ls-files '*.go')`
3. `$(go env GOPATH)/bin/goimports -l -local github.com/Viking602/go-hydaelyn $(git ls-files '*.go')`
4. `git diff --check`

The second and third commands must print no files. `git diff --check` must print nothing.

## GoLand Settings

In GoLand, keep Go code style aligned with the command-line contract:

- use tabs for Go indentation
- enable Optimize imports when reformatting Go files
- use goimports-style import grouping: standard library, third-party packages, then local project packages
- enable gofmt on GoLand's Reformat Code action
- keep Go Linter configured to use `.golangci.yml`
- keep the project inspection profile enabled for repository scans; do not use a personal global profile as the review standard

## Verification Gate

A format-only pass is not complete until these commands pass:

```bash
go test ./...
go vet ./...
/opt/homebrew/bin/golangci-lint run --timeout=5m
$(go env GOPATH)/bin/staticcheck ./...
$(go env GOPATH)/bin/govulncheck ./...
git diff --check
```

GoLand inspection is complete only when this headless check writes no problem files besides `.descriptions.json`:

```bash
/Users/viking/Applications/GoLand.app/Contents/bin/inspect.sh \
  /Users/viking/GolandProjects/go-hydaelyn \
  /Users/viking/GolandProjects/go-hydaelyn/.goland/inspection-profile.xml \
  /tmp/go-hydaelyn-inspection-results \
  -format json -v1
```

For this repository, GoLand default inspection output contains intentional library-project noise such as exported public API symbols that are not used internally, Markdown snippet false positives, compatibility-field deprecation warnings, Markdown table preferences, and project vocabulary/grammar warnings. The project inspection profile disables those categories after review. Do not delete public API, rewrite docs, or rename exported symbols just to silence those categories.
