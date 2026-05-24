-- storage/postgres reference implementation schema (v0.8.0).
-- Single-file canonical DDL; MigrationsFS() exposes it for external tools.
-- Mirrors the SQLite schema; integer columns are widened to BIGINT so that
-- unix-nanoseconds and 64-bit lease versions fit without truncation.
-- Payload columns stay TEXT for cross-dialect parity — the JSON shape is
-- defined by the api.* types, not by the database. Switch to JSONB in a
-- downstream migration if you need server-side JSON queries.

CREATE TABLE IF NOT EXISTS runs (
    id          TEXT PRIMARY KEY,
    status      TEXT,
    agent_id    TEXT,
    payload     TEXT NOT NULL,
    created_at  BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    run_id      TEXT NOT NULL,
    id          TEXT NOT NULL,
    status      TEXT,
    payload     TEXT NOT NULL,
    PRIMARY KEY (run_id, id)
);

CREATE TABLE IF NOT EXISTS events (
    run_id      TEXT NOT NULL,
    sequence    BIGINT NOT NULL,
    type        TEXT,
    payload     TEXT NOT NULL,
    recorded_at BIGINT NOT NULL,
    PRIMARY KEY (run_id, sequence)
);

CREATE TABLE IF NOT EXISTS trace_spans (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL,
    payload     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_trace_spans_run ON trace_spans(run_id);

CREATE TABLE IF NOT EXISTS blackboard_items (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL,
    item_type   TEXT,
    payload     TEXT NOT NULL,
    created_at  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_blackboard_run ON blackboard_items(run_id);

CREATE TABLE IF NOT EXISTS user_messages (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL,
    recipient   TEXT,
    status      TEXT NOT NULL,
    payload     TEXT NOT NULL,
    created_at  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_user_messages_run_status_created
    ON user_messages(run_id, status, created_at);

CREATE TABLE IF NOT EXISTS envelopes (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL,
    task_id     TEXT NOT NULL,
    payload     TEXT NOT NULL,
    created_at  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_envelopes_run ON envelopes(run_id);

CREATE TABLE IF NOT EXISTS leases (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL,
    task_id     TEXT NOT NULL,
    holder_id   TEXT NOT NULL,
    status      TEXT NOT NULL,
    version     BIGINT NOT NULL DEFAULT 0,
    payload     TEXT NOT NULL,
    expires_at  BIGINT
);
CREATE INDEX IF NOT EXISTS idx_leases_task ON leases(run_id, task_id, status);

CREATE TABLE IF NOT EXISTS approvals (
    id          TEXT PRIMARY KEY,
    status      TEXT,
    payload     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS resume_tokens (
    id          TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL,
    task_id     TEXT,
    status      TEXT,
    consumed    INTEGER NOT NULL DEFAULT 0,
    payload     TEXT NOT NULL,
    created_at  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_resume_tokens_run
    ON resume_tokens(run_id, consumed, created_at);

CREATE TABLE IF NOT EXISTS action_attempts (
    id          TEXT PRIMARY KEY,
    run_id      TEXT,
    task_id     TEXT,
    payload     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_profiles (
    id          TEXT PRIMARY KEY,
    role        TEXT,
    payload     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS capabilities (
    name        TEXT NOT NULL,
    agent_id    TEXT NOT NULL,
    payload     TEXT NOT NULL,
    PRIMARY KEY (name, agent_id)
);

CREATE TABLE IF NOT EXISTS usage_records (
    id          TEXT PRIMARY KEY,
    run_id      TEXT,
    task_id     TEXT,
    agent_id    TEXT,
    provider    TEXT,
    credits     BIGINT NOT NULL DEFAULT 0,
    payload     TEXT NOT NULL,
    created_at  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_usage_records_run ON usage_records(run_id);

CREATE TABLE IF NOT EXISTS dead_letters (
    id          TEXT PRIMARY KEY,
    envelope_id TEXT NOT NULL,
    run_id      TEXT,
    task_id     TEXT,
    payload     TEXT NOT NULL,
    created_at  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_dead_letters_run ON dead_letters(run_id);
