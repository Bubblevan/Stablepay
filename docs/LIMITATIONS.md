# Known Limitations

These are explicit scope boundaries, not hidden promises:

- Workflow v1 is sequential and has one output sink.
- Workflow does not support compensation, dynamic branching, parallel payment execution, or workflow-level memory.
- The current settlement path is single-currency per Episode; there is no FX layer.
- Initial merchant selection is deterministic; LLM is recovery-only.
- Real payment fault injection is incomplete. Payment faults require a Kitex-aware controller; the generic HTTP fault proxy cannot prove a live Payment Service fault.
- `live_local` latency uses deterministic local adapters and is not real Internet, Payment Service, Solana, or DeepSeek latency.
- The committed S11 example is replay data, not a live benchmark.
- A real DeepSeek claim requires a persisted non-rule `ModelDecisionTrace`; a happy path without that trace is reported as no LLM call.
- A real Devnet claim requires a recorded network and transaction evidence. F0 does not rerun paid Devnet traffic automatically.
- The S5 approval schema retains the historical `ALLOW_SWITCH` scope name for the parent-confirmation reason; this is documented naming debt and does not grant an unrequested switch.
- The Runtime readiness probe checks composition, supervisors, and MySQL. It does not assert that every downstream service has been actively probed.
- The project is research/portfolio engineering code; no production-traffic, QPS, availability, or zero-failure claim is made.
