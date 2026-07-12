-- 开发环境：清空 StablePay 四个业务库里的所有业务表（仍不 DROP DATABASE）
-- 会删除 DID 注册，OpenClaw 需重新 stablepay_register_local_did
-- 仅在确认要「完全重来」时使用

SET FOREIGN_KEY_CHECKS = 0;

USE stablepay_did_db;
TRUNCATE TABLE did_identities;

USE stablepay_verification_db;
TRUNCATE TABLE purchase_records;

USE stablepay_payment_db;
TRUNCATE TABLE payment_transactions;
TRUNCATE TABLE payment_idempotency_keys;
TRUNCATE TABLE blockchain_callbacks;
TRUNCATE TABLE gas_subsidies;

USE stablepay_query_db;
TRUNCATE TABLE transaction_records;

SET FOREIGN_KEY_CHECKS = 1;
