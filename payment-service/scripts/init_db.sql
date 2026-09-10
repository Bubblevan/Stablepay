-- Payment Service 数据库初始化脚本
-- 数据库: stablepay_payment_db

CREATE DATABASE IF NOT EXISTS `stablepay_payment_db`
    DEFAULT CHARACTER SET utf8mb4
    DEFAULT COLLATE utf8mb4_unicode_ci;

USE `stablepay_payment_db`;

-- 支付交易表
CREATE TABLE IF NOT EXISTS `payment_transactions` (
    `id`              BIGINT UNSIGNED     AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    `tx_id`           VARCHAR(64)         NOT NULL COMMENT '交易ID (UUID)',
    `agent_did`       VARCHAR(128)        NOT NULL COMMENT '支付方DID',
    `skill_did`       VARCHAR(128)        NOT NULL COMMENT '收款方Skill DID',
    `amount`          BIGINT              NOT NULL COMMENT '支付金额 (USDC最小单位，6位小数)',
    `currency`        VARCHAR(10)         NOT NULL DEFAULT 'USDC' COMMENT '币种: USDC/USDT',
    `signature`       VARCHAR(512)        NOT NULL COMMENT '支付签名(base58)',
    `sign_timestamp`  BIGINT              NOT NULL COMMENT '签名时间戳',
    `sign_nonce`      VARCHAR(64)         NOT NULL COMMENT '签名随机数(防重放)',
    `tx_hash`         VARCHAR(128)        DEFAULT NULL COMMENT '链上交易哈希',
    `status`          TINYINT             NOT NULL DEFAULT 0 COMMENT '状态: 0=CREATED, 1=PENDING, 2=CONFIRMED, 3=COMPLETED, 4=FAILED, 5=CANCELLED',
    `retry_count`     TINYINT             NOT NULL DEFAULT 0 COMMENT '重试次数',
    `error_code`      VARCHAR(32)         DEFAULT NULL COMMENT '错误码',
    `error_message`   VARCHAR(512)        DEFAULT NULL COMMENT '错误信息',
    `created_at`      DATETIME            NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at`      DATETIME            NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    `confirmed_at`    DATETIME            DEFAULT NULL COMMENT '确认时间',
    `request_id`      VARCHAR(128)         DEFAULT NULL COMMENT '请求关联ID',
    `trace_id`        VARCHAR(128)         DEFAULT NULL COMMENT '链路关联ID',
    `expires_at`      DATETIME            NOT NULL COMMENT '超时时间',

    UNIQUE KEY `uk_tx_id` (`tx_id`),
    UNIQUE KEY `uk_sign_nonce` (`sign_nonce`),
    KEY `idx_agent_status` (`agent_did`, `status`),
    KEY `idx_skill_status` (`skill_did`, `status`),
    KEY `idx_status_created` (`status`, `created_at`),
    KEY `idx_tx_hash` (`tx_hash`),
    KEY `idx_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='支付交易表';

-- 幂等性控制表
CREATE TABLE IF NOT EXISTS `payment_idempotency_keys` (
    `id`              BIGINT UNSIGNED     AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    `idempotency_key` VARCHAR(128)        NOT NULL COMMENT '幂等性键 (agent_did:skill_did:client_key)',
    `tx_id`           VARCHAR(64)         DEFAULT NULL COMMENT '关联的交易ID',
    `status`          TINYINT             NOT NULL DEFAULT 0 COMMENT '状态: 0=PENDING, 1=COMPLETED, 2=FAILED',
    `request_hash`    VARCHAR(64)         NOT NULL COMMENT '请求参数哈希(确保相同key对应相同请求)',
    `response_data`   JSON                DEFAULT NULL COMMENT '响应数据缓存',
    `expires_at`      DATETIME            NOT NULL COMMENT '过期时间',
    `created_at`      DATETIME            NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',

    UNIQUE KEY `uk_idempotency_key` (`idempotency_key`),
    KEY `idx_expires_at` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='幂等性控制表';

-- 链上回调记录表
CREATE TABLE IF NOT EXISTS `blockchain_callbacks` (
    `id`              BIGINT UNSIGNED     AUTO_INCREMENT PRIMARY KEY COMMENT '主键ID',
    `tx_hash`         VARCHAR(128)        NOT NULL COMMENT '链上交易哈希',
    `tx_id`           VARCHAR(64)         NOT NULL COMMENT '关联的交易ID',
    `callback_type`   VARCHAR(32)         NOT NULL COMMENT '回调类型: confirmation/failure',
    `callback_data`   JSON                NOT NULL COMMENT '回调数据',
    `processed`       TINYINT             NOT NULL DEFAULT 0 COMMENT '是否已处理: 0=否, 1=是',
    `created_at`      DATETIME            NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `processed_at`    DATETIME            DEFAULT NULL COMMENT '处理时间',

    UNIQUE KEY `uk_tx_hash_type` (`tx_hash`, `callback_type`),
    KEY `idx_tx_id` (`tx_id`),
    KEY `idx_processed_created` (`processed`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='链上回调记录表';
