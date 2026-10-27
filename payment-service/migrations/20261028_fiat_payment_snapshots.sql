-- Apply before enabling FIAT_HTTP_ADDR outside the local environment.
-- Separate CNY provider state; existing chain payment rows are unchanged.
CREATE TABLE IF NOT EXISTS fiat_payment_snapshots (
  id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  payload LONGBLOB NOT NULL
) ENGINE=InnoDB;
INSERT IGNORE INTO fiat_payment_snapshots (id,payload) VALUES (1,'{}');
