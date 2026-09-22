# Development History

This file keeps milestone history out of the user-facing architecture description. The current system is described by its final boundaries, not by milestone names.

| Line | Evidence in history |
|---|---|
| S0-S4 | Contract, Episode, catalog, merchant, payment, entitlement, validator, ledger, and deterministic runtime foundations in the preceding repository history |
| S5 | `83f69cf` recovery and parent escalation; `c80e238` recovery authority and replay hardening |
| S6 | `8a7bf7c` LLM decision and evidence boundary |
| S7 | `e78bf98`, `e2dcdd4`, `66ec12c` persistent memory substrate, hardening, scope/ranking/provenance |
| Unified U1/U2 | `9086871`, `b62e062`, `1fccccf`, `b423752` reproducible real-business and real-provider acceptance paths |
| Runtime surface | `08e09b9` and predecessor `6535c87` external HTTP/MCP/CLI composition and E2E surface |
| Eval / observability | `190c11b`, `8e88216`, `dd3e0cd`, `cea47a6` external harness, integrity semantics, and isolated live-local reporting |
| Workflow | `ef6dc0d`, `fcd1954`, `ac2dc51` declarative workflow, artifact data plane, and final hardening |

There is no mandatory S9 milestone in the freeze manifest. S12, RL/GRPO, self-evolution, dynamic workflow planning, parallel workflow execution, and new Memory/Payment/Recovery features are future ideas only and are outside the frozen project.
