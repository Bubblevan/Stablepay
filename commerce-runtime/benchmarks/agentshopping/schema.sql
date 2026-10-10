-- Benchmark-only isolated commerce simulation. Never apply to StablePay's
-- production schema or point this package at production credentials.
CREATE TABLE IF NOT EXISTS sim_tenants (
    tenant_id VARCHAR(64) NOT NULL PRIMARY KEY,
    label VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sim_products (
    tenant_id VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    sku_id VARCHAR(128) NOT NULL,
    category VARCHAR(128) NOT NULL,
    title VARCHAR(512) NOT NULL,
    item_name VARCHAR(128) NOT NULL,
    format_name VARCHAR(128) NOT NULL,
    style_name VARCHAR(128) NOT NULL,
    currency CHAR(4) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, merchant_id, sku_id),
    CONSTRAINT fk_sim_products_tenant FOREIGN KEY (tenant_id)
        REFERENCES sim_tenants (tenant_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sim_inventory (
    tenant_id VARCHAR(64) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    sku_id VARCHAR(128) NOT NULL,
    region VARCHAR(32) NOT NULL,
    on_hand INT NOT NULL,
    reserved INT NOT NULL DEFAULT 0,
    eligible BOOLEAN NOT NULL,
    price_minor BIGINT NOT NULL,
    revision BIGINT NOT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, merchant_id, sku_id, region),
    CONSTRAINT chk_sim_inventory_counts CHECK (on_hand >= 0 AND reserved >= 0 AND reserved <= on_hand),
    CONSTRAINT chk_sim_inventory_price CHECK (price_minor >= 0 AND revision >= 1),
    CONSTRAINT fk_sim_inventory_product FOREIGN KEY (tenant_id, merchant_id, sku_id)
        REFERENCES sim_products (tenant_id, merchant_id, sku_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sim_quotes (
    quote_id CHAR(32) NOT NULL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    task_id VARCHAR(64) NOT NULL,
    quote_version BIGINT NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    sku_id VARCHAR(128) NOT NULL,
    region VARCHAR(32) NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency CHAR(4) NOT NULL,
    inventory_revision BIGINT NOT NULL,
    issued_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_sim_quote_scope (tenant_id, quote_id),
    UNIQUE KEY uq_sim_quote_version (tenant_id, merchant_id, sku_id, region, quote_version),
    KEY ix_sim_quotes_task (tenant_id, task_id, issued_at),
    CONSTRAINT chk_sim_quote_amount CHECK (amount_minor >= 0 AND quote_version >= 1),
    CONSTRAINT fk_sim_quotes_inventory FOREIGN KEY (tenant_id, merchant_id, sku_id, region)
        REFERENCES sim_inventory (tenant_id, merchant_id, sku_id, region)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sim_confirmations (
    confirmation_id CHAR(32) NOT NULL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    quote_id CHAR(32) NOT NULL,
    quote_version BIGINT NOT NULL,
    amount_minor BIGINT NOT NULL,
    accepted BOOLEAN NOT NULL,
    confirmed_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_sim_confirmation_scope (tenant_id, confirmation_id),
    KEY ix_sim_confirmations_quote (tenant_id, quote_id),
    CONSTRAINT fk_sim_confirmation_quote FOREIGN KEY (tenant_id, quote_id)
        REFERENCES sim_quotes (tenant_id, quote_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sim_orders (
    order_id CHAR(32) NOT NULL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    task_id VARCHAR(64) NOT NULL,
    quote_id CHAR(32) NOT NULL,
    confirmation_id CHAR(32) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6) NULL,
    UNIQUE KEY uq_sim_order_idempotency (tenant_id, idempotency_key),
    UNIQUE KEY uq_sim_order_scope (tenant_id, order_id),
    CONSTRAINT fk_sim_order_quote FOREIGN KEY (tenant_id, quote_id)
        REFERENCES sim_quotes (tenant_id, quote_id),
    CONSTRAINT fk_sim_order_confirmation FOREIGN KEY (tenant_id, confirmation_id)
        REFERENCES sim_confirmations (tenant_id, confirmation_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sim_reservations (
    reservation_id CHAR(32) NOT NULL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    order_id CHAR(32) NOT NULL,
    merchant_id VARCHAR(64) NOT NULL,
    sku_id VARCHAR(128) NOT NULL,
    region VARCHAR(32) NOT NULL,
    quantity INT NOT NULL,
    status VARCHAR(16) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6) NULL,
    UNIQUE KEY uq_sim_reservation_order (tenant_id, order_id),
    CONSTRAINT chk_sim_reservation_qty CHECK (quantity > 0),
    CONSTRAINT fk_sim_reservation_order FOREIGN KEY (tenant_id, order_id)
        REFERENCES sim_orders (tenant_id, order_id),
    CONSTRAINT fk_sim_reservation_inventory FOREIGN KEY (tenant_id, merchant_id, sku_id, region)
        REFERENCES sim_inventory (tenant_id, merchant_id, sku_id, region)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sim_mock_payments (
    payment_id CHAR(32) NOT NULL PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    order_id CHAR(32) NOT NULL,
    amount_minor BIGINT NOT NULL,
    currency CHAR(4) NOT NULL,
    provider VARCHAR(32) NOT NULL,
    status VARCHAR(16) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    UNIQUE KEY uq_sim_mock_payment_order (tenant_id, order_id),
    CONSTRAINT chk_sim_mock_payment_amount CHECK (amount_minor >= 0),
    CONSTRAINT chk_sim_mock_provider CHECK (provider = 'benchmark-mock'),
    CONSTRAINT fk_sim_mock_payment_order FOREIGN KEY (tenant_id, order_id)
        REFERENCES sim_orders (tenant_id, order_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sim_audit_events (
    event_id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    tenant_id VARCHAR(64) NOT NULL,
    task_id VARCHAR(64) NOT NULL,
    entity_id CHAR(32) NOT NULL,
    action VARCHAR(40) NOT NULL,
    details JSON NULL,
    created_at DATETIME(6) NOT NULL,
    KEY ix_sim_audit_task (tenant_id, task_id, event_id),
    KEY ix_sim_audit_entity (tenant_id, entity_id, event_id),
    CONSTRAINT fk_sim_audit_tenant FOREIGN KEY (tenant_id)
        REFERENCES sim_tenants (tenant_id)
) ENGINE=InnoDB;
