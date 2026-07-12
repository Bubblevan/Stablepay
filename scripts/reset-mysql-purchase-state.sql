-- StablePay 开发环境：仅清理「购买 / 支付 / 验证」相关表
-- 不删 DID 注册、不删库结构。执行后需重新支付才能 unlock 商家技能。
--
-- 影响：
--   stablepay_verification_db  → GET /api/v1/verify 变为 purchased:false
--   stablepay_payment_db       → 可再次 POST /api/v1/pay（幂等键清空）
--   stablepay_payment_db.gas_subsidies → adapter 补贴流水清空
--   stablepay_query_db         → 账单/流水展示清空（不影响链上余额）

SET FOREIGN_KEY_CHECKS = 0;

-- ========== verification-service（verify 读这里）==========
USE stablepay_verification_db;
TRUNCATE TABLE purchase_records;
-- 若有 X 验证表（GORM: x_verifications）
-- TRUNCATE TABLE x_verifications;

-- ========== payment-service（支付记录 + 幂等）==========
USE stablepay_payment_db;
TRUNCATE TABLE payment_transactions;
TRUNCATE TABLE payment_idempotency_keys;
TRUNCATE TABLE blockchain_callbacks;
TRUNCATE TABLE gas_subsidies;

-- ========== query-service（统计/流水，可选）==========
USE stablepay_query_db;
TRUNCATE TABLE transaction_records;

SET FOREIGN_KEY_CHECKS = 1;

SELECT 'stablepay_verification_db.purchase_records' AS tbl, COUNT(*) AS rows FROM stablepay_verification_db.purchase_records
UNION ALL
SELECT 'stablepay_payment_db.payment_transactions', COUNT(*) FROM stablepay_payment_db.payment_transactions
UNION ALL
SELECT 'stablepay_payment_db.gas_subsidies', COUNT(*) FROM stablepay_payment_db.gas_subsidies;
