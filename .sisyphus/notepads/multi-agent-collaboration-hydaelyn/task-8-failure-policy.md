# Task 8 - failure policy routing

- Runtime task outcome application now rejects stale results when the authoritative task version has changed or the current task is already terminal.
- Blocking failures (`fail_fast`, exhausted `retry`) now abort transitive dependents immediately, while `degrade` and `skip_optional` leave unrelated work runnable.
- Direct `skip_optional` execution failures now normalize to `skipped`; cancellation-propagated failures normalize to `aborted` so late worker results cannot overwrite them.
- Planner replan now reconciles tasks by ID: matching tasks are superseded in place with `Version+1`, omitted non-terminal tasks are marked `aborted`, and fresh tasks are appended.
- Queue persistence now treats superseded/terminal tasks as authoritative during stale-write conflict handling so late leased results are ignored deterministically.
