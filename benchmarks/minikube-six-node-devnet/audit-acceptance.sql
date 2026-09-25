-- Read-only cross-service audit for the single low-value Devnet acceptance episode.
-- Run against the local six-node Minikube MySQL instance.
USE commerce_runtime_k8s_e2e;
SET @episode_id = 'ce_9fc2b5eddfcbc645249fbd677baf6dd4';
SET @payment_intent_id = 'pi_5080dd2ffc573a32bd67e94a1df36302';
SET @tx_id = '1fe3e0d7-250b-4a7a-8f28-3f324935b506';

-- Episode must be terminal and execution complete.
SELECT e.episode_id, e.state, x.status AS execution_status, x.last_error_code
FROM commerce_episodes AS e
LEFT JOIN episode_execution_status AS x ON x.episode_id = e.episode_id
WHERE e.episode_id = @episode_id;

-- Exactly one durable PaymentIntent; duplicate query must return zero rows.
SELECT COUNT(*) AS payment_intent_count
FROM payment_intents WHERE episode_id = @episode_id;
SELECT episode_id, COUNT(*) AS row_count
FROM payment_intents WHERE episode_id = @episode_id
GROUP BY episode_id HAVING COUNT(*) > 1;

-- Exactly one settlement; duplicate query must return zero rows.
SELECT payment_intent_id, amount_minor, tx_id
FROM ledger_entries
WHERE episode_id = @episode_id AND type = 'PAYMENT_SETTLED';
SELECT payment_intent_id, COUNT(*) AS row_count
FROM ledger_entries
WHERE episode_id = @episode_id AND type = 'PAYMENT_SETTLED'
GROUP BY payment_intent_id HAVING COUNT(*) > 1;

-- Expected ledger-derived budget projection, followed by persisted projection:
-- expected consumed, available and sunk cost must equal actual values.
SELECT e.budget_limit_minor,
       IF(e.refund_reusable = 0, l.settled,
          GREATEST(0, l.settled - l.refunded)) AS expected_consumed,
       e.budget_limit_minor
         - IF(e.refund_reusable = 0, l.settled,
              GREATEST(0, l.settled - l.refunded))
         - (l.reserved - l.released) AS expected_available,
       GREATEST(0, l.settled - l.refunded) AS expected_sunk_cost,
       e.consumed_amount AS actual_consumed,
       e.available_budget AS actual_available,
       e.sunk_cost AS actual_sunk_cost
FROM commerce_episodes AS e
JOIN (
    SELECT COALESCE(SUM(CASE WHEN type = 'BUDGET_RESERVED' THEN amount_minor ELSE 0 END), 0) AS reserved,
           COALESCE(SUM(CASE WHEN type = 'BUDGET_RELEASED' THEN amount_minor ELSE 0 END), 0) AS released,
           COALESCE(SUM(CASE WHEN type = 'PAYMENT_SETTLED' THEN amount_minor ELSE 0 END), 0) AS settled,
           COALESCE(SUM(CASE WHEN type = 'REFUND_CONFIRMED' THEN amount_minor ELSE 0 END), 0) AS refunded
    FROM ledger_entries WHERE episode_id = @episode_id
) AS l
WHERE e.episode_id = @episode_id;

-- Payment Service and Verification Service must agree on the same confirmed tx.
SELECT tx_id, status, amount, currency, tx_hash, request_id
FROM stablepay_payment_db.payment_transactions WHERE tx_id = @tx_id;
SELECT tx_id, amount_minor, currency, tx_hash, event_id
FROM stablepay_verification_db.purchase_records WHERE tx_id = @tx_id;

-- Merchant delivery and validator evidence.
SELECT phase, attempt, response_status, entitlement_ref
FROM merchant_invocations WHERE episode_id = @episode_id ORDER BY started_at;
SELECT valid, reason_code, validator_name, validator_version, payload_hash
FROM validation_evidence WHERE episode_id = @episode_id;
