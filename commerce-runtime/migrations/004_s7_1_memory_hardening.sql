-- S7.1 deterministic memory substrate hardening. Memory remains advisory only.
ALTER TABLE memory_records ADD COLUMN IF NOT EXISTS catalog_snapshot_ref VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE memory_records DROP INDEX uk_memory_identity, ADD UNIQUE KEY uk_memory_identity (type, scope, requester_did, parent_session_id, merchant_did, capability_id, catalog_version, catalog_snapshot_hash);
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS catalog_snapshot_ref VARCHAR(255) NULL;
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS trigger_event_ref VARCHAR(255) NULL;
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS recovery_action_event_ref VARCHAR(255) NULL;
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS terminal_event_ref VARCHAR(255) NULL;
ALTER TABLE memory_observations ADD COLUMN IF NOT EXISTS related_event_refs JSON NULL;

CREATE TABLE IF NOT EXISTS memory_use_traces (
    memory_use_trace_id       VARCHAR(128) NOT NULL PRIMARY KEY,
    episode_id                VARCHAR(128) NOT NULL,
    model_decision_trace_id   VARCHAR(255) NOT NULL,
    context_hash              CHAR(71) NOT NULL,
    retrieved_memory_refs     JSON NULL,
    cited_memory_refs         JSON NULL,
    proposed_action           VARCHAR(64) NOT NULL,
    guard_accepted            BOOLEAN NOT NULL,
    created_at                DATETIME(6) NOT NULL,
    facts_ref                 VARCHAR(255) NOT NULL,
    payload_hash              CHAR(71) NOT NULL,
    KEY idx_memory_use_trace_episode (episode_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
