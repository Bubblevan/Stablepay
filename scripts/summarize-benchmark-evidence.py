#!/usr/bin/env python3
"""Read-only B0 evidence summarizer.

The script intentionally emits candidate claims and rejected claims; it never
edits resume documents and never invents a percentage when an arm is missing.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def load(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {"status": "NOT RUN", "missing": str(path)}
    try:
        return json.loads(path.read_text(encoding="utf-8-sig"))
    except (OSError, json.JSONDecodeError) as exc:
        return {"status": "INVALID EVIDENCE", "path": str(path), "error": str(exc)}


def rate(value: dict[str, Any]) -> dict[str, Any]:
    return {
        "numerator": value.get("numerator", 0),
        "denominator": value.get("denominator", 0),
        "rate": value.get("rate"),
        "wilson_95_low": value.get("wilson_95_low"),
        "wilson_95_high": value.get("wilson_95_high"),
    }


def resolve_e4_full_run(run_dir: Path) -> Path:
    """Resolve an explicitly selected E4 run, never an arbitrary latest run."""
    run_dir = run_dir.expanduser().resolve()
    full_dir = run_dir if run_dir.name.lower() == "full" else run_dir / "full"
    required = (full_dir / "metrics.json", full_dir / "audit-summary.json")
    missing = [str(path) for path in required if not path.is_file()]
    if missing:
        raise SystemExit(
            "FAIL / no audited full E4 evidence; --e4-run must name an E4 run "
            "directory containing full/ or the full/ directory itself. Missing: "
            + ", ".join(missing)
        )
    return full_dir


def relative_evidence(path: Path, root: Path) -> str:
    try:
        return path.resolve().relative_to(root.resolve()).as_posix()
    except ValueError:
        return path.resolve().as_posix()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".local-run/resume-benchmark", type=Path)
    parser.add_argument(
        "--e4-run",
        required=True,
        type=Path,
        help="explicit E4 run directory (containing full/) or its full/ directory; must include metrics.json and audit-summary.json",
    )
    args = parser.parse_args()
    root = args.root
    e4_run = resolve_e4_full_run(args.e4_run)
    experiments = {
        "e1-recovery": load(root / "e1-recovery" / "metrics.json"),
        "e2-guard": load(root / "e2-guard" / "metrics.json"),
        "e3-steady-state-final": load(root / "e3-steady-state-final" / "metrics.json"),
        "e4": load(e4_run / "metrics.json"),
    }
    e4_audit = load(e4_run / "audit-summary.json")
    manifests = {
        "e1-recovery": load(root / "e1-recovery" / "manifest.json"),
        "e2-guard": load(root / "e2-guard" / "manifest.json"),
        "e3-steady-state-final": load(root / "e3-steady-state-final" / "manifest.json"),
        "e4": load(e4_run / "manifest.json"),
    }
    claims: list[dict[str, Any]] = []
    rejected: list[dict[str, Any]] = []

    e2 = experiments["e2-guard"]
    if e2.get("status") == "COMPLETE" and e2.get("false_accept", {}).get("numerator") == 0 and e2.get("side_effect_escape_count") == 0:
        claims.append({
            "claim": "RuntimeGuard accepted valid proposals and rejected unsafe proposals on the frozen positive/negative dataset without observed side-effect escape.",
            "evidence": ["e2-guard/metrics.json", "e2-guard/results.jsonl", "e2-guard/report.md"],
            "numerator": {"unsafe_rejection": rate(e2.get("unsafe_proposal_rejection", {})), "valid_acceptance": rate(e2.get("valid_proposal_acceptance", {})), "side_effect_escape": 0},
            "limitations": ["E2b is a live-local CommitProposal-boundary check; it is not a production-traffic guarantee."]
        })
    else:
        rejected.append({"experiment": "E2", "reason": "No trustworthy zero-false-accept evidence was available; do not make a Guard accuracy claim.", "evidence": ["e2-guard/metrics.json"]})

    e1 = experiments["e1-recovery"]
    rule = e1.get("rule", {})
    deepseek = e1.get("deepseek", {})
    rejected.append({
        "experiment": "E1",
        "classification": "INTERVIEW_ONLY_NEGATIVE_ABLATION" if deepseek.get("status") == "RUN" else "INTERVIEW_ONLY_NOT_RUN",
        "resume_claim": False,
        "reason": "E1 is intentionally excluded from resume claims. The observed DeepSeek treatment did not outperform Rule; retain only as an interview discussion of a negative ablation." if deepseek.get("status") == "RUN" else "E1 real DeepSeek treatment was not run; no resume claim is permitted.",
        "numerator": {"rule_recovery_success": rate(rule.get("recovery_success", {})), "deepseek_recovery_success": rate(deepseek.get("recovery_success", {}))},
        "evidence": ["e1-recovery/metrics.json", "e1-recovery/report.md"],
    })

    e3 = experiments["e3-steady-state-final"]
    e3_matrix = e3.get("matrix", [])
    e3_peak = max(e3_matrix, key=lambda tier: tier.get("concurrency", 0), default={})
    e3_measurement = e3_peak.get("measurement", {})
    e3_http_p95_ms = e3_measurement.get("http_req_duration_p95_ms")
    e3_episode_p95_ms = e3_measurement.get("episode_e2e_latency_p95_ms")
    e3_expected_vus = {1, 10, 25, 50, 100, 200}
    e3_matrix_passed = (
        len(e3_matrix) == len(e3_expected_vus)
        and {tier.get("concurrency") for tier in e3_matrix} == e3_expected_vus
        and all(
        tier.get("exit_code") == 0
        and not tier.get("thresholds_failed", True)
        and tier.get("measurement", {}).get("http_req_failed_rate", 1) == 0
        and tier.get("measurement", {}).get("business_error_rate", 1) == 0
        for tier in e3_matrix
        )
        and isinstance(e3_measurement.get("http_requests_per_second"), (int, float))
        and isinstance(e3_measurement.get("completed_episodes_per_second"), (int, float))
        and isinstance(e3_http_p95_ms, (int, float))
        and isinstance(e3_episode_p95_ms, (int, float))
    )
    if e3.get("status") == "COMPLETE" and e3_matrix_passed:
        e3_http_rps_display = f"{e3_measurement.get('http_requests_per_second'):,.0f}"
        e3_episode_rate_display = f"{e3_measurement.get('completed_episodes_per_second'):.1f}"
        e3_http_p95_display = f"{e3_http_p95_ms:.1f} ms"
        e3_episode_p95_display = f"{e3_episode_p95_ms / 1000:.3f} s"
        claims.append({
            "claim": f"Load-tested the authenticated Runtime HTTP boundary to {e3_peak.get('concurrency')} VUs, measuring {e3_http_rps_display} HTTP req/s and {e3_episode_rate_display} completed Episodes/s, with HTTP p95 {e3_http_p95_display} and Episode p95 {e3_episode_p95_display} at 0% observed HTTP/business errors.",
            "evidence": ["e3-steady-state-final/manifest.json", "e3-steady-state-final/metrics.json", "e3-steady-state-final/report.md"],
            "numerator": {"vus": e3_peak.get("concurrency"), "http_requests_per_second": e3_measurement.get("http_requests_per_second"), "completed_episodes_per_second": e3_measurement.get("completed_episodes_per_second"), "http_p95_ms": e3_measurement.get("http_req_duration_p95_ms"), "episode_p95_ms": e3_measurement.get("episode_e2e_latency_p95_ms"), "http_error_rate": e3_measurement.get("http_req_failed_rate"), "business_error_rate": e3_measurement.get("business_error_rate")},
            "limitations": ["One local Windows host with Docker k6, MySQL, and deterministic local dependencies; not production or Internet/provider latency."]
        })
    else:
        rejected.append({"experiment": "E3", "reason": "No fully passing completed load matrix is available; no QPS/latency claim.", "evidence": ["e3-steady-state-final/manifest.json", "e3-steady-state-final/metrics.json"]})

    e4 = experiments["e4"]
    e4_trials = e4.get("trial_count", 0)
    audit_counts_zero = all(
        key in e4
        and audit_key in e4_audit
        and e4[key] == 0
        and e4_audit[audit_key] == 0
        for key, audit_key in (
            ("duplicate_payment_intent_rows", "duplicate_payment_intent_rows"),
            ("duplicate_PAYMENT_SETTLED_rows", "duplicate_payment_settled_rows"),
            ("payment_intent_count_mismatch", "payment_intent_count_mismatch"),
            ("payment_settled_count_mismatch", "payment_settled_count_mismatch"),
            ("budget_drift", "budget_drift"),
            ("orphaned_episodes", "orphaned_episodes"),
            ("stuck_episodes", "stuck_episodes"),
        )
    )
    e4_audit_passed = (
        e4_audit.get("status") == "PASS"
        and e4_audit.get("trials") == e4_trials
        and e4_audit.get("passed") == e4_trials
        and e4_audit.get("failed") == 0
        and audit_counts_zero
    )
    e4_resume = e4.get("resume_success", {})
    if e4.get("status") == "COMPLETE" and e4.get("correctness_status") == "PASS" and e4.get("FROZEN_RUNTIME_CHANGED") == "NO" and e4_resume.get("numerator") == e4_trials and e4_resume.get("denominator") == e4_trials and e4_audit_passed:
        e4_ttr = e4.get("ttr_seconds", {})
        claims.append({
            "claim": f"Validated crash/restart recovery across {len(e4.get('crash_windows', []))} persisted Runtime windows: {e4_resume.get('numerator')}/{e4_trials} episodes resumed and completed, with MySQL audit PASS and zero duplicate intents/settlements, payment-count mismatches, budget drift, orphaned or stuck Episodes; TTR P50/P95 {e4_ttr.get('p50')}/{e4_ttr.get('p95')} s.",
            "evidence": [relative_evidence(e4_run / "manifest.json", root), relative_evidence(e4_run / "trials.jsonl", root), relative_evidence(e4_run / "metrics.json", root), relative_evidence(e4_run / "audit-summary.json", root), relative_evidence(e4_run / "audit-report.md", root)],
            "numerator": {"resume_success": rate(e4.get("resume_success", {})), "ttr_seconds": e4.get("ttr_seconds"), "crash_windows": e4.get("crash_windows"), "audit": {"status": e4_audit.get("status"), "passed": e4_audit.get("passed"), "failed": e4_audit.get("failed")}, "duplicate_payment_intent_rows": e4_audit.get("duplicate_payment_intent_rows"), "duplicate_PAYMENT_SETTLED_rows": e4_audit.get("duplicate_payment_settled_rows"), "payment_intent_count_mismatch": e4_audit.get("payment_intent_count_mismatch"), "payment_settled_count_mismatch": e4_audit.get("payment_settled_count_mismatch"), "budget_drift": e4_audit.get("budget_drift"), "orphaned_episodes": e4_audit.get("orphaned_episodes"), "stuck_episodes": e4_audit.get("stuck_episodes"), "FROZEN_RUNTIME_CHANGED": e4.get("FROZEN_RUNTIME_CHANGED")},
            "limitations": ["Benchmark-only deterministic local merchant/payment/verification adapters with an isolated MySQL schema; not a real payment-network fault test."]
        })
    else:
        rejected.append({"experiment": "E4", "reason": "Explicit E4 full run is incomplete, correctness is not PASS, frozen runtime changed, or the matching full-run audit is not PASS; no resume claim.", "evidence": [relative_evidence(e4_run / "manifest.json", root), relative_evidence(e4_run / "metrics.json", root), relative_evidence(e4_run / "audit-summary.json", root)]})

    environment = {}
    for name, manifest in manifests.items():
        if manifest.get("environment"):
            environment[name] = manifest["environment"]
    output = {
        "benchmark": "B0",
        "version": "v1",
        "candidate_claims": claims,
        "claims_rejected": rejected,
        "evidence": {name: value.get("evidence", []) for name, value in experiments.items()},
        "environment": environment,
        "manifests": {name: {key: value.get(key) for key in ("benchmark", "version", "dataset_hash", "git_sha", "runtime_variant", "seed", "status")} for name, value in manifests.items()},
        "limitations": ["This file is generated from existing evidence only.", "It does not edit docs/RESUME_VARIANTS.md.", "Missing, NOT RUN, or audit-pending experiments cannot support headline claims."],
    }
    root.mkdir(parents=True, exist_ok=True)
    output_path = root / "resume-safe-summary.json"
    output_path.write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(output_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
