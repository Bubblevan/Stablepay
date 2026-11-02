CREATE TABLE IF NOT EXISTS machine_pay_orders (
  id VARCHAR(32) NOT NULL PRIMARY KEY,
  idempotency_key VARCHAR(128) NOT NULL,
  resource_id VARCHAR(32) NOT NULL,
  goods_name VARCHAR(32) NOT NULL,
  amount_minor BIGINT NOT NULL,
  payment_link VARCHAR(32) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL,
  trade_no VARCHAR(80) NULL,
  created_at DATETIME(3) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  paid_at DATETIME(3) NULL,
  UNIQUE KEY uidx_machinepay_idempotency (resource_id, idempotency_key),
  UNIQUE KEY uidx_machinepay_trade (trade_no),
  KEY idx_machinepay_status (status),
  KEY idx_machinepay_created_at (created_at),
  KEY idx_machinepay_expires_at (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS machine_pay_outbox (
  event_id VARCHAR(128) NOT NULL PRIMARY KEY,
  payload LONGBLOB NOT NULL,
  created_at DATETIME(3) NOT NULL,
  published_at DATETIME(3) NULL,
  KEY idx_machinepay_outbox_created_at (created_at),
  KEY idx_machinepay_outbox_published_at (published_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
