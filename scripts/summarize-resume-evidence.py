#!/usr/bin/env python3
"""Create a resume-safe summary from an existing S11 live-local run.

This script is deliberately read-only with respect to Runtime and benchmark
execution. It requires the local evidence directory and never falls back to
README/doc numbers when the source artifacts are missing.
"""

from __future__ import annotations

import argparse
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any


SAFETY_NEGATIVE_TASK_IDS = {"guard-rejected"}
REQUIRED_FILES = ("episode_results.jsonl", "grade_results.jsonl", "metrics.json", "report.md")


def fail(message: str) -> "NoReturn":
    raise SystemExit(f"FAIL / no evidence: {message}")


def read_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        fail(f"cannot read {path}: {exc}")


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        fail(f"cannot read {path}: {exc}")
    values: list[dict[str, Any]] = []
    for line_number, line in enumerate(lines, start=1):
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError as exc:
            fail(f"invalid JSONL at {path}:{line_number}: {exc}")
        if not isinstance(value, dict):
            fail(f"JSONL value at {path}:{line_number} is not an object")
        values.append(value)
    if not values:
        fail(f"empty evidence file: {path}")
    return values


def key(value: dict[str, Any]) -> tuple[Any, ...]:
    return (value.get("trial_isolation_id"), value.get("task_id"), value.get("trial_index"))


def passed(value: dict[str, Any]) -> bool:
    return bool((value.get("grade") or {}).get("passed"))


def assertion_passed(value: dict[str, Any], name: str) -> bool:
    for assertion in (value.get("grade") or {}).get("assertions", []):
        if assertion.get("name") == name:
            return bool(assertion.get("passed"))
    return False


def guard_rejected(value: dict[str, Any]) -> bool:
    trace = value.get("trace") or {}
    for decision in trace.get("decision_outcome_traces") or []:
        if decision.get("stage") == "RUNTIME_GUARD" and decision.get("guard_accepted") is False:
            return True
    for model_trace in trace.get("model_decision_traces") or []:
        if model_trace.get("status") == "GUARD_REJECTED":
            return True
    return False


def summarize_group(values: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        "trials": len(values),
        "passed": sum(1 for value in values if passed(value)),
        "failure_kinds": sorted({(value.get("failure") or {}).get("kind", "none") for value in values}),
        "paths": sorted({value.get("path", "") for value in values}),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root",
        default=".local-run/s11-live-local",
        help="existing S11 live-local evidence directory",
    )
    args = parser.parse_args()
    root = Path(args.root)
    if not root.is_dir():
        fail(f"missing evidence directory: {root}")
    for filename in REQUIRED_FILES:
        if not (root / filename).is_file():
            fail(f"missing required evidence file: {root / filename}")

    metrics = read_json(root / "metrics.json")
    results = read_jsonl(root / "episode_results.jsonl")
    grades = read_jsonl(root / "grade_results.jsonl")

    result_keys = [key(value) for value in results]
    grade_keys = [key(value) for value in grades]
    if len(set(result_keys)) != len(result_keys):
        fail("duplicate trial identity in episode_results.jsonl")
    if set(result_keys) != set(grade_keys):
        fail("episode and grade JSONL trial identities do not match")
    if not metrics.get("evidence", {}).get("sensitive_fields_omitted", False):
        fail("metrics evidence does not attest sensitive-field omission")

    task_ids = {value.get("task_id") for value in results}
    if not SAFETY_NEGATIVE_TASK_IDS.issubset(task_ids):
        fail("expected safety-negative task taxonomy is absent")

    functional = [value for value in results if value.get("task_id") not in SAFETY_NEGATIVE_TASK_IDS]
    safety_negative = [value for value in results if value.get("task_id") in SAFETY_NEGATIVE_TASK_IDS]
    triggered_faults = [value for value in results if (value.get("failure") or {}).get("triggered") and (value.get("failure") or {}).get("injection_count", 0) > 0]
    recovery = [value for value in results if value.get("path") == "recovery"]

    task_groups: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for value in results:
        task_groups[str(value.get("task_id", "unknown"))].append(value)

    safety_clean = [
        value
        for value in safety_negative
        if guard_rejected(value)
        and assertion_passed(value, "unsafe_side_effect_count")
        and assertion_passed(value, "duplicate_settlement_count")
    ]

    fault_by_kind: dict[str, dict[str, int]] = {}
    for kind in sorted({(value.get("failure") or {}).get("kind", "none") for value in triggered_faults}):
        values = [value for value in triggered_faults if (value.get("failure") or {}).get("kind", "none") == kind]
        fault_by_kind[kind] = {
            "triggered": len(values),
            "grade_passed": sum(1 for value in values if passed(value)),
        }

    group_pass8 = sum(1 for values in task_groups.values() if values and all(passed(value) for value in values))

    evidence = metrics.get("evidence", {})
    summary = {
        "schema_version": 1,
        "source": {
            "directory": str(root).replace("\\", "/"),
            "required_files": list(REQUIRED_FILES),
            "generated_at": metrics.get("generated_at"),
            "mode": sorted({value.get("mode") for value in results}),
            "environment": sorted({value.get("environment") for value in results}),
            "runtime_variants": sorted({((value.get("trace") or {}).get("runtime_variant") or {}).get("runtime_version") for value in results}),
        },
        "trial_integrity": {
            "submitted": len(results),
            "valid": evidence.get("valid_trials"),
            "collection_error_count": evidence.get("collection_error_count"),
            "sensitive_fields_omitted": evidence.get("sensitive_fields_omitted"),
        },
        "functional": {
            **summarize_group(functional),
            "success_numerator": sum(1 for value in functional if passed(value)),
            "success_denominator": len(functional),
        },
        "safety_negative": {
            "task_ids": sorted(SAFETY_NEGATIVE_TASK_IDS),
            **summarize_group(safety_negative),
            "guard_rejected": sum(1 for value in safety_negative if guard_rejected(value)),
            "side_effect_safe": len(safety_clean),
            "duplicate_settlement_safe": sum(1 for value in safety_negative if assertion_passed(value, "duplicate_settlement_count")),
        },
        "recovery": {
            "trials": len(recovery),
            "passed": sum(1 for value in recovery if passed(value)),
            "fault_triggered": len(triggered_faults),
            "fault_recovered": sum(1 for value in triggered_faults if passed(value)),
            "fault_by_kind": fault_by_kind,
        },
        "task_groups": {task_id: summarize_group(values) for task_id, values in sorted(task_groups.items())},
        "pass8": {"passing_task_groups": group_pass8, "eligible_task_groups": len(task_groups)},
        "observed_safety": {
            "unsafe_side_effect_count": metrics.get("unsafe_side_effect_count"),
            "duplicate_settlement_count": metrics.get("duplicate_settlement_count"),
        },
        "latency_ms": {
            "episode_p50": metrics.get("episode_latency_p50_ms"),
            "episode_p95": metrics.get("episode_latency_p95_ms"),
            "llm_p50": metrics.get("llm_latency_p50_ms"),
            "llm_p95": metrics.get("llm_latency_p95_ms"),
            "payment_confirmation_p50": metrics.get("payment_confirmation_latency_p50_ms"),
            "payment_confirmation_p95": metrics.get("payment_confirmation_latency_p95_ms"),
            "recovery_p50": metrics.get("recovery_latency_p50_ms"),
            "recovery_p95": metrics.get("recovery_latency_p95_ms"),
            "limitation": "deterministic live-local adapters; not Internet, Payment Service, Solana, or DeepSeek latency",
        },
        "claims_boundary": {
            "real_llm": "NOT RUN in F0",
            "real_devnet": "NOT RUN in F0",
            "headline_task_success_rate": "intentionally omitted; functional and safety-negative groups are separated",
        },
    }
    output = root / "resume-safe-summary.json"
    output.write_text(json.dumps(summary, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"PASS / wrote {output}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        sys.exit(130)
