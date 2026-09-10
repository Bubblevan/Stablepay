# Payment event contract

Payment events are JSON messages on the RocketMQ physical topic `payment_events`.
`event_type` is the event kind inside that topic; it is not another physical topic.

| Meaning | Value |
|---|---|
| Physical topic | `payment_events` |
| Successful event type/tag | `payment.success` |
| Failed event type/tag | `payment.failed` |
| Schema version | `1` |
| Delivery | at least once |

The canonical payload is represented by `examples/events/payment.success.json` and
`payment.failed.json`. Required semantics are:

- `event_id`: stable event identity. The producer derives it deterministically from payment ID and event type, so a republish of the same state has the same identity.
- `event_type` and `schema_version`: event kind and version of this contract.
- `idempotency_key`: the payment request identity (`agent_did:skill_did:nonce` in the payment aggregate).
- `tx_id`, `agent_did`, `skill_did`, `amount_minor`, `currency`, `tx_hash`, `status`.
- `occurred_at` and optional `confirmed_at`: RFC3339 timestamps.
- optional `request_id` and `trace_id`: correlation values copied from the payment RPC base request and retained in the payment row.

`topic` is intentionally absent from the payload because the broker topic is configured
out-of-band and duplicating it created two competing meanings. RocketMQ tags carry the
same `event_type` value.

`verification-service` validates the envelope, stores `event_id` as a nullable unique
column on `purchase_records`, and checks it before projection. The existing unique
`(agent_did, skill_did)` business invariant remains a second guard for legacy rows.
Transient decode, connection, and database failures return RocketMQ retry status; a
duplicate event is acknowledged successfully. This is the at-least-once boundary.
