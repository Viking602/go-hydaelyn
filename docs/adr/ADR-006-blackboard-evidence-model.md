# ADR-006 Blackboard / Evidence Data Model

## Status

Accepted

## Context

Previously, multi-agent collaboration relied solely on `task.Result` and concatenating the final summary:

- the research task exposed its summary directly to the final aggregation
- the verify task had no structured output
- the synthesizer could not distinguish verified from unverified conclusions

This left the runtime unable to express the real relationships between claim, evidence, and verification.

## Decision

- Introduce the `blackboard` package, defining:
  - `Source`
  - `Artifact`
  - `Evidence`
  - `Claim`
  - `Finding`
  - `VerificationResult`
- after a research task completes, the runtime publishes to the blackboard through the publish pipeline
- the publish pipeline handles the minimal version:
  - `normalize`
  - `dedupe`
  - `redact`
  - `score`
- after a verify task completes, it produces a structured `VerificationResult`
- the synthesizer only consumes the finding corresponding to a `supported` claim

## Current Semantics

- a research task publishes source, artifact, evidence, claim, and finding
- a verify task writes a `VerificationResult` keyed to the claim of the research task it depends on
- in the requireVerification scenario, deepsearch only aggregates supported findings

## Impact

- the final output no longer depends directly on the raw worker summary
- contradiction / insufficient have entered a trackable state model
- subsequent verifier plugin, approval, and event replay now all have a stable place to land their data
