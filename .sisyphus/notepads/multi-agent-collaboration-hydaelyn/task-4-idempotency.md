# Task 4 - queue lease idempotency

- Added `team.Task.CompletedAt` and `CompletedBy` plus `HasAuthoritativeCompletion()` so queued workers can distinguish execution-finished tasks from the single committed completion path.
- Runtime outcome application now short-circuits once a task already has an authoritative completion, preventing duplicate blackboard publications or later retries from overwriting the committed result.
- Queued lease processing now keeps the heartbeat alive until CAS-backed persistence resolves, and only releases the lease after a successful save or after confirming another worker already committed the same task.
- Queued persistence now treats stale CAS failures as benign when the reloaded team state shows the same task already completed, which turns lease-expiry races into idempotent no-ops instead of surfacing hard worker errors.
- Added regressions for both in-memory queued retry idempotency and a distributed lease-expiry race where two workers execute the same task but only one completion is published.
