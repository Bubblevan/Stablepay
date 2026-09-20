-- S6 bounded evidence and redacted model-call traces. Retrieved text is
-- explanatory data and is never a substitute for structured runtime facts.
CREATE TABLE IF NOT EXISTS evidence_records (
    evidence_id    VARCHAR(128) NOT NULL PRIMARY KEY,
    evidence_ref   VARCHAR(255) NOT NULL UNIQUE,
    source_type    VARCHAR(64) NOT NULL,
    source_ref     VARCHAR(255) NOT NULL,
    source_version VARCHAR(128) NOT NULL,
    source_hash    CHAR(71) NOT NULL,
    content_type   VARCHAR(128) NOT NULL,
    payload_hash   CHAR(71) NOT NULL,
    chunk_hash     CHAR(71) NOT NULL,
    trust_class    VARCHAR(32) NOT NULL,
    content        LONGTEXT NOT NULL,
    created_at     DATETIME(6) NOT NULL,
    valid_until    DATETIME(6) NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS model_decision_traces (
    trace_id             VARCHAR(255) NOT NULL PRIMARY KEY,
    episode_id           VARCHAR(128) NOT NULL,
    provider             VARCHAR(64) NOT NULL,
    model_ref            VARCHAR(128) NOT NULL,
    context_hash         CHAR(71) NOT NULL,
    evidence_refs        JSON NULL,
    request_started_at   DATETIME(6) NOT NULL,
    response_received_at DATETIME(6) NOT NULL,
    raw_response_hash    CHAR(71) NULL,
    parsed_proposal_hash CHAR(71) NULL,
    status               VARCHAR(32) NOT NULL,
    error_code           VARCHAR(64) NULL,
    input_tokens         INT NOT NULL DEFAULT 0,
    output_tokens        INT NOT NULL DEFAULT 0,
    fallback_reason      VARCHAR(255) NULL,
    KEY idx_model_trace_episode (episode_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
