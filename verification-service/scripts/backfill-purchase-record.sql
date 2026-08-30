-- 链上已成功但 payment-service 因 POLL_TIMEOUT 标 FAILED、verify 仍 false 时，可手工补一条购买记录
-- 使用前请确认 getSignatureStatuses / Solscan 上 tx 已 finalized 且 err 为 null

USE stablepay_verification_db;

INSERT INTO purchase_records (created_at, updated_at, deleted_at, agent_did, skill_did, tx_id)
VALUES (
  NOW(3),
  NOW(3),
  NULL,
  'did:solana:2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF',
  'did:solana:4p8F5YAJM8fdrNyvWfb3p6XHx8rboFVV3xn279VXo2j7',
  '3d60cf80-8fcf-4406-b2fd-376154d6d1ad'
)
ON DUPLICATE KEY UPDATE
  tx_id = VALUES(tx_id),
  updated_at = NOW(3);

SELECT * FROM purchase_records
WHERE agent_did = 'did:solana:2gL5tHKBp2ZWGX9chgcHFtEqLcwG2GT76CimizqFqXXF'
  AND skill_did = 'did:solana:4p8F5YAJM8fdrNyvWfb3p6XHx8rboFVV3xn279VXo2j7';
