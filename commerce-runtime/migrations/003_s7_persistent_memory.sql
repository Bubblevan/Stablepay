-- S7 derived historical memory. It is advisory context, never transactional authority.
ALTER TABLE model_decision_traces ADD COLUMN IF NOT EXISTS memory_refs JSON NULL;

CREATE TABLE IF NOT EXISTS memory_records (
    memory_id              VARCHAR(128) NOT NULL PRIMARY KEY,
    type                   VARCHAR(48) NOT NULL,
    scope                  VARCHAR(48) NOT NULL,
    requester_did          VARCHAR(128) NOT NULL,
    parent_session_id      VARCHAR(128) NOT NULL,
    merchant_did           VARCHAR(128) NOT NULL,
    capability_id          VARCHAR(128) NOT NULL,
    catalog_version        VARCHAR(64) NOT NULL,
    catalog_snapshot_hash  CHAR(71) NOT NULL,
    summary                VARCHAR(1024) NOT NULL,
    structured_facts       JSON NOT NULL,
    source_episode_ids     JSON NOT NULL,
    source_event_refs      JSON NOT NULL,
    source_evidence_refs   JSON NULL,
    observation_count      INT NOT NULL,
    confidence             DECIMAL(8,6) NOT NULL,
    first_observed_at      DATETIME(6) NOT NULL,
    last_observed_at       DATETIME(6) NOT NULL,
    valid_from             DATETIME(6) NOT NULL,
    valid_until            DATETIME(6) NULL,
    created_at             DATETIME(6) NOT NULL,
    updated_at             DATETIME(6) NOT NULL,
    facts_ref              VARCHAR(255) NOT NULL,
    payload_hash           CHAR(71) NOT NULL,
    UNIQUE KEY uk_memory_identity (type, scope, requester_did, parent_session_id, merchant_did, capability_id, catalog_version),
    KEY idx_memory_last_observed (last_observed_at),
    KEY idx_memory_valid_until (valid_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS memory_observations (
    observation_id         VARCHAR(128) NOT NULL PRIMARY KEY,
    memory_id              VARCHAR(128) NOT NULL,
    source_episode_id      VARCHAR(128) NOT NULL,
    source_event_ref       VARCHAR(255) NOT NULL,
    source_evidence_refs   JSON NULL,
    observation_kind       VARCHAR(64) NOT NULL,
    outcome                VARCHAR(128) NULL,
    observed_at            DATETIME(6) NOT NULL,
    catalog_version        VARCHAR(64) NULL,
    catalog_snapshot_hash  CHAR(71) NULL,
    payload_hash           CHAR(71) NOT NULL,
    UNIQUE KEY uk_memory_observation_identity (memory_id, source_episode_id, observation_kind),
    KEY idx_memory_observation_memory (memory_id),
    CONSTRAINT fk_memory_observation_record FOREIGN KEY (memory_id) REFERENCES memory_records(memory_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
