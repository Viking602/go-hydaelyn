# Hydaelyn Product Specification

Source-of-truth design documents for Hydaelyn, organized by release.

## Versions

| Version | Status | Theme |
|---------|--------|-------|
| [v0.8.0](./v0.8.0/) | **Active** — implementation in progress | Public Framework Release: durable storage, Capability layer, AgentProfile extension, Worker Runtime, governance, four-layer context, eval framework |
| [v0.9.0](./v0.9.0/) | Roadmap stub | Memory pipeline (L0→L3), symbolic short-term context (Mermaid canvas), knowledge-graph context source, vector retrieval, Postgres LISTEN/NOTIFY-based Subscribe |

## Conventions

- Each version directory is self-contained: every doc inside `v0.8.0/` describes what v0.8.0 ships, not what is planned next.
- Cross-version references use the form `[doc 07 in v0.9.0](./v0.9.0/07-context.md)` so links remain unambiguous.
- Decisions land via ADRs in `../adr/`. The version directory's `09-boundaries.md` and `11-rollout-plan.md` enumerate which ADRs that version introduces.

## Read order for new contributors

1. Start at the active version's [README](./v0.8.0/README.md).
2. Skim `09-boundaries.md` first — every other doc operates under those principles.
3. Then follow the dependency order listed in `00-overview.md`.
