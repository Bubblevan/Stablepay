CREATE TABLE IF NOT EXISTS fiat_payment_store_locks (
  id BIGINT UNSIGNED NOT NULL PRIMARY KEY
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT IGNORE INTO fiat_payment_store_locks (id) VALUES (1);

CREATE TABLE IF NOT EXISTS fiat_payment_orders (
  id VARCHAR(40) NOT NULL PRIMARY KEY,
  request_id VARCHAR(128) NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  product_id VARCHAR(128) NOT NULL,
  subject VARCHAR(180) NOT NULL,
  quote_hash VARCHAR(128) NOT NULL,
  approval_ref VARCHAR(128) NOT NULL,
  agent_did VARCHAR(255) NOT NULL,
  skill_did VARCHAR(255) NOT NULL,
  amount_minor BIGINT NOT NULL,
  currency VARCHAR(8) NOT NULL,
  mode VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL,
  trade_no VARCHAR(80) NULL,
  verified_by VARCHAR(40) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  paid_at DATETIME(3) NULL,
  harness_policy VARCHAR(64) NOT NULL DEFAULT '',
  UNIQUE KEY uidx_fiat_order_request (request_id),
  UNIQUE KEY uidx_fiat_order_idempotency (idempotency_key),
  UNIQUE KEY uidx_fiat_order_trade (trade_no),
  KEY idx_fiat_payment_orders_status (status),
  KEY idx_fiat_payment_orders_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS fiat_payment_outbox (
  event_id VARCHAR(128) NOT NULL PRIMARY KEY,
  payload LONGBLOB NOT NULL,
  created_at DATETIME(3) NOT NULL,
  published_at DATETIME(3) NULL,
  KEY idx_fiat_payment_outbox_created_at (created_at),
  KEY idx_fiat_payment_outbox_published_at (published_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
