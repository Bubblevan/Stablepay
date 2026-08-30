# TASK-01 to TASK-05 Implementation Map

This file turns `TASK-01` through `TASK-05` from the TRD into concrete artifacts in this directory.

## TASK-01 Failure Taxonomy Contract

Input:

- `docs/TRD.md`
- current plugin trace format

Output:

- `schemas/failure-taxonomy.schema.json`

Verification:

- every failed task can emit one `primary_error_category`
- passed tasks must emit `primary_error_category = "none"`
- evidence list is mandatory

Risk:

- if categories overlap, failure analysis will become noisy

## TASK-02 Latency Event Schema

Input:

- live-vs-mock execution mode split
- need to distinguish Agent slow vs backend slow

Output:

- `schemas/latency-breakdown.schema.json`

Verification:

- supports all six target latency layers
- allows `null` for timestamps not observable in mock mode

Risk:

- some live timestamps may require later harness instrumentation to populate accurately

## TASK-03 Task JSON v2 Schema

Input:

- existing task JSON shape in plugin eval harness
- richer final-state and adversarial requirements

Output:

- `schemas/task-v2.schema.json`
- sample task: `tasks/samples/pay_small_item_v2.json`

Verification:

- sample task expresses fixtures, tool constraints, argument assertions, final-state assertions, and latency expectations in one document

Risk:

- schema can become hard to author manually if too many special cases are embedded directly

## TASK-04 Normalized Fixture Formats

Input:

- current plugin state mismatch issue
- backend precondition variability

Output:

- plugin fixtures:
  - `fixtures/plugin_state/templates/wallet_absent.json`
  - `fixtures/plugin_state/templates/did_absent.json`
  - `fixtures/plugin_state/templates/ready_state.json`
  - `fixtures/plugin_state/templates/key_state_mismatch.json`
- backend fixtures:
  - `fixtures/backend_state/templates/merchant_labubu_0_2_clean.json`
  - `fixtures/backend_state/templates/duplicate_replay_clean.json`

Verification:

- fixture names are stable and human-readable
- key mismatch is represented explicitly as normalized metadata, not as opaque encrypted state

Risk:

- if later harness logic starts depending on raw encrypted plugin state, portability will break

## TASK-05 Final-State Assertion Adapter Contract

Input:

- payment DB schema
- purchase record persistence
- query read model

Output:

- `schemas/state-assertion.schema.json`

Verification:

- supports:
  - `sql_count`
  - `query_field_equals`
  - `balance_delta`
  - `timeout_lte`
  - `final_status`

Risk:

- real adapter implementation still needs environment-specific execution code for SQL and query checks

## What Is Done vs Not Done Yet

Done now:

- schema contracts
- normalized fixture templates
- sample task contract
- implementation mapping for TASK-01 to TASK-05

Still pending in later steps:

- harness code changes to load these schemas
- live SQL/query assertion executor
- real latency instrumentation hooks
- first full task batch and report generation
