-- storage/mysql reference implementation schema (v0.8.0).
-- Targets MySQL 8.0+, MariaDB 10.5+, TiDB 6+, and OceanBase 4.x (MySQL
-- mode). The schema avoids JSON_TABLE, JSON_OVERLAPS, generated columns,
-- and AUTO_INCREMENT primary keys to maximize compat across that fleet.
--
-- Payload columns are LONGTEXT (not JSON) so the schema is uniform across
-- engines that lack full JSON support. Switch to native JSON in a
-- downstream migration when you are on a single-engine deployment.
--
-- All tables are InnoDB + utf8mb4 with the default _0900_ai_ci / _general_ci
-- collation chosen by the server. ID columns are VARCHAR(191) so they fit
-- under the utf8mb4 unique-key length cap on older MySQL builds.

CREATE TABLE IF NOT EXISTS runs (
    id          VARCHAR(191) NOT NULL,
    status      VARCHAR(64),
    agent_id    VARCHAR(191),
    payload     LONGTEXT NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS tasks (
    run_id      VARCHAR(191) NOT NULL,
    id          VARCHAR(191) NOT NULL,
    status      VARCHAR(64),
    payload     LONGTEXT NOT NULL,
    PRIMARY KEY (run_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS events (
    run_id      VARCHAR(191) NOT NULL,
    sequence    BIGINT NOT NULL,
    type        VARCHAR(128),
    payload     LONGTEXT NOT NULL,
    recorded_at BIGINT NOT NULL,
    PRIMARY KEY (run_id, sequence)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS trace_spans (
    id          VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191) NOT NULL,
    payload     LONGTEXT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_trace_spans_run (run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS blackboard_items (
    id          VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191) NOT NULL,
    item_type   VARCHAR(64),
    payload     LONGTEXT NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_blackboard_run (run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS user_messages (
    id          VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191) NOT NULL,
    recipient   VARCHAR(191),
    status      VARCHAR(64) NOT NULL,
    payload     LONGTEXT NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_user_messages_run_status_created (run_id, status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS envelopes (
    id          VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191) NOT NULL,
    task_id     VARCHAR(191) NOT NULL,
    payload     LONGTEXT NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_envelopes_run (run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS leases (
    id          VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191) NOT NULL,
    task_id     VARCHAR(191) NOT NULL,
    holder_id   VARCHAR(191) NOT NULL,
    status      VARCHAR(64) NOT NULL,
    version     BIGINT NOT NULL DEFAULT 0,
    payload     LONGTEXT NOT NULL,
    expires_at  BIGINT,
    PRIMARY KEY (id),
    KEY idx_leases_task (run_id, task_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS approvals (
    id          VARCHAR(191) NOT NULL,
    status      VARCHAR(64),
    payload     LONGTEXT NOT NULL,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS resume_tokens (
    id          VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191) NOT NULL,
    task_id     VARCHAR(191),
    status      VARCHAR(64),
    consumed    TINYINT NOT NULL DEFAULT 0,
    payload     LONGTEXT NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_resume_tokens_run (run_id, consumed, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS action_attempts (
    id          VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191),
    task_id     VARCHAR(191),
    payload     LONGTEXT NOT NULL,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS agent_profiles (
    id          VARCHAR(191) NOT NULL,
    role        VARCHAR(128),
    payload     LONGTEXT NOT NULL,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS capabilities (
    name        VARCHAR(191) NOT NULL,
    agent_id    VARCHAR(191) NOT NULL,
    payload     LONGTEXT NOT NULL,
    PRIMARY KEY (name, agent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS usage_records (
    id          VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191),
    task_id     VARCHAR(191),
    agent_id    VARCHAR(191),
    provider    VARCHAR(128),
    credits     BIGINT NOT NULL DEFAULT 0,
    payload     LONGTEXT NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_usage_records_run (run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS dead_letters (
    id          VARCHAR(191) NOT NULL,
    envelope_id VARCHAR(191) NOT NULL,
    run_id      VARCHAR(191),
    task_id     VARCHAR(191),
    payload     LONGTEXT NOT NULL,
    created_at  BIGINT NOT NULL,
    PRIMARY KEY (id),
    KEY idx_dead_letters_run (run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
