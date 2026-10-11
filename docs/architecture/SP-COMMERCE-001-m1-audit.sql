-- Read-only audit for the deterministic M1 Runtime/MySQL mock E2E.
-- Use only against the isolated stablepay_commerce_m1_test database.
SET @episode_id = 'ce_115a0f277682ff112c635ed4dea0b009';

-- Episode state and budget projection.
SELECT episode_id, request_id, contract_snapshot_hash, state,
       current_payment_requirement_id, current_quote_hash,
       budget_limit_minor, reserved_amount, settled_amount, consumed_amount,
       available_budget, sunk_cost
FROM commerce_episodes
WHERE episode_id = @episode_id;

-- Buyer decisions must each bind to a persisted immutable payment requirement.
SELECT bc.confirmation_id, bc.offer_ref, bc.quote_hash, bc.amount_minor,
       bc.currency, bc.accepted, pr.payment_requirement_id AS matched_requirement_id
FROM buyer_confirmations AS bc
LEFT JOIN payment_requirement_facts AS pr
  ON pr.episode_id = bc.episode_id
 AND pr.payment_requirement_id = bc.offer_ref
WHERE bc.episode_id = @episode_id
ORDER BY bc.created_at, bc.offer_ref;

-- Count distinct economic identities and inspect the one Runtime intent.
SELECT COUNT(*) AS payment_intent_count,
       COUNT(DISTINCT economic_key) AS economic_identity_count,
       SUM(status = 'CONFIRMED') AS confirmed_intent_count
FROM payment_intents
WHERE episode_id = @episode_id;
SELECT intent_id, payment_requirement_id, quote_hash, amount_minor, currency,
       status, tx_id, economic_key
FROM payment_intents
WHERE episode_id = @episode_id
ORDER BY created_at, intent_id;

-- Ledger facts and projection inputs.
SELECT sequence, type, currency, amount_minor, payment_intent_id, tx_id,
       idempotency_key
FROM ledger_entries
WHERE episode_id = @episode_id
ORDER BY sequence;

-- Delivery must reference the same PaymentIntent and have valid evidence.
SELECT da.delivery_id, da.payment_intent_id, da.payload_hash, da.http_status,
       ve.validation_id, ve.valid, ve.reason_code
FROM delivery_artifacts AS da
LEFT JOIN validation_evidence AS ve ON ve.delivery_id = da.delivery_id
WHERE da.episode_id = @episode_id
ORDER BY da.received_at, da.delivery_id;

-- Must return zero: no buyer confirmation is detached from its quoted offer.
SELECT COUNT(*) AS confirmations_without_requirement
FROM buyer_confirmations AS bc
LEFT JOIN payment_requirement_facts AS pr
  ON pr.episode_id = bc.episode_id
 AND pr.payment_requirement_id = bc.offer_ref
WHERE bc.episode_id = @episode_id
  AND pr.payment_requirement_id IS NULL;

-- Retained negative cases: these must have no buyer confirmation or economic facts.
SELECT e.episode_id, e.state,
       (SELECT COUNT(*) FROM buyer_confirmations AS bc WHERE bc.episode_id = e.episode_id) AS buyer_confirmations,
       (SELECT COUNT(*) FROM payment_intents AS pi WHERE pi.episode_id = e.episode_id) AS payment_intents,
       (SELECT COUNT(*) FROM ledger_entries AS le WHERE le.episode_id = e.episode_id) AS ledger_entries
FROM commerce_episodes AS e
WHERE e.episode_id IN (
    'ce_c9b8118be66550d4f901fee3418dc773',
    'ce_ace730bbc5b30bc1aecf6c4d1b06088a'
)
ORDER BY e.episode_id;
