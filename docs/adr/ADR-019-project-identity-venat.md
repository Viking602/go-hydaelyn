# ADR-019 Project Identity — Venat

## Status

Accepted — effective from v0.12.0 (2026-07-31).

## Context

The project began under the `Hydaelyn` / `go-hydaelyn` identity. That name is
strongly associated with Final Fantasy XIV and Square Enix, while this module is
an independent Go framework. The existing identity also appears in the GitHub
repository, Go module path, root package, CLI, documentation, prompts, local
configuration directories, and a small number of persisted wire values.

A partial visual rebrand would leave two public identities in normal use. For Go,
the module directive is the canonical dependency identity: changing it creates a
new module path even when GitHub redirects the old repository URL. The cutover
therefore needs one canonical identity and an explicit migration boundary.

## Decision

### 1. Canonical identity

Starting with v0.12.0, the canonical project identity is:

| Surface | Canonical value |
| --- | --- |
| Product and repository | `Venat` / `Viking602/venat` |
| Go module | `github.com/Viking602/venat` |
| Root package | `venat` |
| Root facade file | `venat.go` |
| CLI package and binary | `cmd/venat` / `venat` |
| Local skill directory | `.venat/skills` |

All current examples, release automation, badges, active documentation, and
internal imports use that identity.

### 2. Clean module and API cutover

The project does not ship a compatibility package, import alias, `replace`
directive, or duplicate CLI. Consumers migrate by changing the module/import
prefix from `github.com/Viking602/go-hydaelyn` to
`github.com/Viking602/venat`, changing the root qualifier from `hydaelyn` to
`venat`, and changing CLI invocations from `hydaelyn` to `venat`.

This is a deliberate pre-v1 breaking change. Existing `go-hydaelyn` versions
remain immutable under their published tags. The renamed GitHub repository keeps
GitHub's redirect for the old repository URL, which allows those historical
versions to remain retrievable while the redirect exists. New releases are
published only from the `github.com/Viking602/venat` module path.

### 3. Runtime-owned identity

Framework-generated prompts and process-local test/helper identifiers use
`Venat` or `VENAT_*`. Skill discovery scans `.venat/skills` as the conventional
product-owned location. It does not silently merge `.hydaelyn/skills`; when that
legacy directory exists, discovery emits a diagnostic directing the user to move
it. Explicit caller-provided skill directories remain unaffected.

### 4. Persisted wire compatibility

The following identifiers remain unchanged because transcripts, tool allowlists,
and stored messages may outlive the module or repository name:

- `hydaelyn_activate_skill`
- `hydaelyn_read_skill_resource`
- `hydaelyn.skill.context`

They are wire contracts, not current product branding. Renaming them would make
stored conversations unreplayable and could invalidate external allowlists.
Tests pin these exact values.

### 5. Historical records

Versioned product specifications, accepted ADR text, and release notes retain the
name and module path that were true when they were published. Active guides use
Venat. This ADR and `docs/migration.md` provide the boundary between the two
identities.

## Anti-patterns rejected by this ADR

- **Cosmetic-only rename:** changing headings while leaving the old module,
  package, or CLI as the normal entrypoint.
- **Dual canonical modules:** publishing equivalent current releases under both
  module paths.
- **Hidden compatibility shim:** keeping undocumented aliases or duplicate
  binaries that make the supported identity ambiguous.
- **Persisted-key churn:** renaming stored protocol identifiers solely for visual
  consistency.
- **Silent configuration merge:** loading both `.venat/skills` and
  `.hydaelyn/skills` with unclear precedence.

## Impact

- Every consumer must update imports and the root package qualifier for v0.12.0.
- CLI installations move to `github.com/Viking602/venat/cmd/venat`.
- Existing `.hydaelyn/skills` content requires an explicit directory move.
- Historical `go-hydaelyn` tags remain useful, but receive no new releases.
- The GitHub repository rename, module cutover, and v0.12.0 tag must happen in
  that order so release verification resolves the canonical path.

## References

- [Migration notes](../migration.md)
- [Go Modules Reference: module paths](https://go.dev/ref/mod#module-path)
- [GitHub: Renaming a repository](https://docs.github.com/en/repositories/creating-and-managing-repositories/renaming-a-repository)
