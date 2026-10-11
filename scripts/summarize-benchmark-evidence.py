#!/usr/bin/env python3
"""Read-only B0 evidence summarizer.

The script intentionally emits candidate claims and rejected claims; it never
edits resume documents and never invents a percentage when an arm is missing.
"""

from __future__ import annotations

import argparse
import hashlib
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


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    try:
        with path.open(encoding="utf-8-sig") as stream:
            for line_number, line in enumerate(stream, 1):
                if not line.strip():
                    raise ValueError(f"blank JSONL row at {path}:{line_number}")
                row = json.loads(line)
                if not isinstance(row, dict):
                    raise ValueError(f"non-object JSONL row at {path}:{line_number}")
                rows.append(row)
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read valid JSONL {path}: {exc}") from exc
    return rows


def resolve_shopping_purchase_run(run_dir: Path) -> Path:
    """Resolve one explicit local replay run; never select an arbitrary latest run."""
    repo_root = Path(__file__).resolve().parents[1]
    evidence_root = (repo_root / ".local-run/agent-shopping/lifecycle-evaluations").resolve()
    resolved = run_dir.expanduser().resolve()
    try:
        resolved.relative_to(evidence_root)
    except ValueError as exc:
        raise ValueError(f"shopping purchase run must be below {evidence_root}: {resolved}") from exc
    required = ("manifest.json", "metrics.json", "audit-summary.json", "trials.jsonl")
    missing = [name for name in required if not (resolved / name).is_file()]
    if missing:
        raise ValueError(f"shopping purchase run is missing required files: {', '.join(missing)}")
    return resolved


def summarize_shopping_purchase_run(run_dir: Path) -> dict[str, Any]:
    """Reconcile the explicit 300-order replay artifacts before emitting a claim."""
    run_dir = resolve_shopping_purchase_run(run_dir)
    paths = {name: run_dir / name for name in ("manifest.json", "metrics.json", "audit-summary.json", "trials.jsonl")}
    manifest = load(paths["manifest.json"])
    metrics = load(paths["metrics.json"])
    audit_summary = load(paths["audit-summary.json"])
    trials = read_jsonl(paths["trials.jsonl"])

    def require(condition: bool, message: str) -> None:
        if not condition:
            raise ValueError(message)

    run_id = run_dir.name
    attempts = 300
    schema_version = manifest.get("schema_version")
    require(schema_version in ("stablepay.agent-shopping-http-purchase-replay.v2", "stablepay.agent-shopping-http-purchase-replay.v3"), "unexpected shopping replay manifest schema")
    retry_schema = schema_version == "stablepay.agent-shopping-http-purchase-replay.v3"
    require(manifest.get("run_id") == run_id and metrics.get("run_id") == run_id, "shopping replay run IDs do not match the selected directory")
    require(manifest.get("status") == "PASS" and metrics.get("status") == "PASS", "shopping replay manifest or metrics are not PASS")
    require(manifest.get("audit_passed") is True and audit_summary.get("passed") is True, "shopping replay audit is not PASS")
    require(manifest.get("task_count") == attempts and metrics.get("logical_purchase_attempts") == attempts and len(trials) == attempts, "shopping replay does not contain exactly 300 attempts")
    require(manifest.get("payment_provider") == "simulation-only" and manifest.get("real_settlement") is False, "shopping replay payment boundary is not simulation-only")
    require(manifest.get("fulfillment_provider") == "simulation-only" and manifest.get("real_fulfillment") is False and manifest.get("real_refund") is False, "shopping replay fulfillment/refund boundary is not simulation-only")
    require(manifest.get("new_model_calls") == 0, "shopping replay unexpectedly made new model calls")

    unique_fields = ("task_id", "order_idempotency_key", "order_id", "payment_id")
    for field in unique_fields:
        values = [str(row.get(field, "")).strip() for row in trials]
        require(all(values) and len(set(values)) == attempts, f"shopping replay {field} values are empty or non-unique")
    require(all(row.get("concurrent_checkout_requests") == 2 for row in trials), "shopping replay did not preserve duplicate checkout pairs")

    fault_counts: dict[str, int] = {}
    for row in trials:
        mode = str(row.get("fault_mode", ""))
        fault_counts[mode] = fault_counts.get(mode, 0) + 1
    expected_faults = {
        "POST_COMMIT_RESPONSE_LOSS_RETRY": 100,
        "POST_COMMIT_RESPONSE_LOSS_API_RECREATE_RETRY": 100,
        "CONCURRENT_DUPLICATE_REPLAY": 100,
    }
    require(fault_counts == expected_faults, "shopping replay fault schedule differs from the declared 100/100/100 matrix")

    settled = declined = delivered = compensated = predeclared = 0
    transient_failures = recovered_faults = provider_attempts = retry_requests = 0
    lost_responses = api_recreations = process_restarts = 0
    for row in trials:
        payment = row.get("mock_payment_status")
        fulfillment = row.get("fulfillment_status")
        refund = row.get("mock_refund_status")
        if payment == "SETTLED":
            settled += 1
            require(row.get("order_status") == "PAID", "settled shopping order is not PAID")
            if fulfillment == "DELIVERED":
                delivered += 1
                require(row.get("final_order_status") == "FULFILLED" and not refund, "delivered shopping order has inconsistent final state")
                if retry_schema:
                    scheduled = row.get("delivery_failure_predeclared") is True
                    row_transient_failures = int(row.get("transient_delivery_failures_observed", 0))
                    row_provider_attempts = int(row.get("fulfillment_provider_attempts", 0))
                    row_retry_requests = int(row.get("fulfillment_retry_http_requests", 0))
                    require(row_transient_failures >= 1 if scheduled else row_transient_failures == 0, "transient fault marker disagrees with observed delivery failures")
                    require(row_provider_attempts == row_transient_failures + 1, "provider attempts do not reconcile with transient failures")
                    require(row_retry_requests >= row_transient_failures, "retry HTTP requests are fewer than transient failures")
                    predeclared += int(scheduled)
                    transient_failures += row_transient_failures
                    recovered_faults += int(scheduled and row_provider_attempts > row_transient_failures)
                    provider_attempts += row_provider_attempts
                    retry_requests += row_retry_requests
                else:
                    require(row.get("delivery_failure_predeclared") is False, "delivered v2 shopping order contradicts its permanent-failure schedule")
            elif fulfillment == "FAILED_COMPENSATED":
                compensated += 1
                predeclared += int(row.get("delivery_failure_predeclared") is True)
                require(not retry_schema, "v3 retry suite unexpectedly compensated a scheduled transient delivery fault")
                require(row.get("final_order_status") == "REFUNDED" and row.get("delivery_failure_predeclared") is True and refund == "REFUNDED", "compensated shopping order has inconsistent refund/final state")
            else:
                raise ValueError("settled shopping order has an unsupported fulfillment state")
        elif payment == "DECLINED":
            declined += 1
            require(row.get("order_status") == "PAYMENT_FAILED" and row.get("final_order_status") == "PAYMENT_FAILED", "declined shopping order has inconsistent final state")
            require(not fulfillment and not refund and row.get("delivery_failure_predeclared") is False, "declined shopping order has unauthorized fulfillment/refund")
        else:
            raise ValueError("shopping trial has an unsupported mock payment status")
        lost_responses += int(row.get("post_commit_response_lost") is True)
        api_recreations += int(row.get("api_recreated_before_retry") is True)
        if row.get("os_process_restarted_before_retry") is True:
            process_restarts += 1
            require(row.get("post_commit_response_lost") is True and row.get("api_recreated_before_retry") is True, "OS process restart trial lacks response-loss/API-recreation faults")

    expected_delivery_counts = (287, 13, 287, 0, 29) if retry_schema else (287, 13, 258, 29, 29)
    require((settled, declined, delivered, compensated, predeclared) == expected_delivery_counts, "shopping replay trial outcomes differ from the audited 300-attempt result")
    require((lost_responses, api_recreations, process_restarts) == (200, 100, 1), "shopping replay response-loss or restart counts differ from the declared schedule")
    require(metrics.get("fulfillment_http_requests") == 3 * settled + retry_requests, "fulfillment HTTP request total does not reconcile with per-order replay/conflict plus retry calls")
    for field, expected in (
        ("unique_orders", attempts),
        ("unique_mock_payments", attempts),
        ("settled_unique_orders", settled),
        ("declined_unique_orders", declined),
        ("fulfilled_orders", delivered),
        ("compensated_delivery_failures", compensated),
        ("injected_post_commit_response_losses", lost_responses),
        ("api_recreations_after_response_loss", api_recreations),
        ("os_process_restarts_after_response_loss", process_restarts),
    ):
        require(metrics.get(field) == expected, f"shopping replay metric {field} does not match trial-derived count")
    if retry_schema:
        for field, expected in (
            ("predeclared_transient_delivery_faults", predeclared),
            ("transient_delivery_failures", transient_failures),
            ("recovered_delivery_faults", recovered_faults),
            ("provider_delivery_attempts", provider_attempts),
            ("fulfillment_retry_http_requests", retry_requests),
        ):
            require(metrics.get(field) == expected, f"shopping replay metric {field} does not match trial-derived count")
    else:
        require(metrics.get("predeclared_delivery_failures") == predeclared, "v2 predeclared delivery-failure count does not match trials")

    audit = metrics.get("audit", {})
    audit_lifecycle = audit.get("lifecycle_audit", {})
    audit_fulfillment = audit.get("fulfillment_audit", {})
    summary_lifecycle = audit_summary.get("lifecycle_audit", {})
    summary_fulfillment = audit_summary.get("fulfillment_audit", {})
    require(audit.get("passed") is True and audit_lifecycle.get("passed") is True and audit_fulfillment.get("passed") is True, "metrics SQL audit is not fully PASS")
    require(summary_lifecycle.get("passed") is True and summary_fulfillment.get("passed") is True, "saved audit-summary lacks a passing lifecycle or fulfillment audit")
    expected_counts = audit_lifecycle.get("expected_counts", {})
    observed_counts = audit_lifecycle.get("observed_counts", {})
    summary_expected_counts = summary_lifecycle.get("expected_counts", {})
    summary_observed_counts = summary_lifecycle.get("observed_counts", {})
    require(bool(expected_counts) and all(observed_counts.get(key) == value for key, value in expected_counts.items()), "shopping lifecycle SQL business-table row counts do not reconcile")
    require(expected_counts == summary_expected_counts and all(summary_observed_counts.get(key) == value for key, value in expected_counts.items()), "metrics and audit-summary lifecycle counts differ")
    require(not audit_lifecycle.get("invariant_violations") and not summary_lifecycle.get("invariant_violations"), "shopping lifecycle SQL audit reports invariant violations")
    require(not audit.get("query_errors") and not audit_summary.get("query_errors"), "shopping SQL audit reports query errors")
    zero_counters = (
        "duplicate_order_idempotency_groups", "duplicate_orders_per_task", "duplicate_payments_per_order",
        "duplicate_settlements_per_order", "amount_or_currency_mismatches",
    )
    for counter in zero_counters:
        require(audit.get(counter) == 0 and audit_summary.get(counter) == 0, f"shopping replay audit counter {counter} is non-zero")
    fulfillment_zero_counters = (
        "duplicate_fulfillments_per_order", "duplicate_refunds_per_order", "settled_payments_without_fulfillment",
        "declined_payments_with_fulfillment_or_refund", "final_order_or_task_state_mismatches",
        "refund_amount_or_payment_mismatches", "reservation_state_mismatches", "non_simulation_fulfillment_effects",
    )
    if retry_schema:
        fulfillment_zero_counters += (
            "duplicate_attempt_ordinals", "attempt_sequence_gaps", "retryable_failures_without_retry",
            "orphan_fulfillment_attempts", "final_attempt_count_mismatches", "terminal_attempt_outcome_mismatches",
            "attempt_idempotency_mismatches",
        )
        require(audit_fulfillment.get("observed_provider_attempts") == provider_attempts, "provider-attempt SQL count differs from trial rows")
        require(audit_fulfillment.get("retryable_failure_attempts") == transient_failures, "retryable-failure SQL count differs from trial rows")
    for counter in fulfillment_zero_counters:
        require(audit_fulfillment.get(counter) == 0 and summary_fulfillment.get(counter) == 0, f"shopping fulfillment audit counter {counter} is non-zero")
    require(not audit_fulfillment.get("query_errors") and not audit_fulfillment.get("invariant_violations"), "shopping fulfillment SQL audit reports errors or invariant violations")

    repo_root = Path(__file__).resolve().parents[1]
    hashes = {
        name: "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()
        for name, path in paths.items()
    }
    evidence = [path.resolve().relative_to(repo_root).as_posix() for path in paths.values()]
    return {
        "run_id": run_id,
        "attempts": attempts,
        "settled": settled,
        "declined": declined,
        "delivered": delivered,
        "compensated_delivery_failures": compensated,
        "predeclared_delivery_failures": predeclared if not retry_schema else 0,
        "predeclared_transient_faults": predeclared if retry_schema else 0,
        "transient_delivery_failures": transient_failures,
        "recovered_delivery_faults": recovered_faults,
        "provider_delivery_attempts": provider_attempts,
        "fulfillment_retry_http_requests": retry_requests,
        "fault_counts": fault_counts,
        "response_losses": lost_responses,
        "api_recreations": api_recreations,
        "os_process_restarts": process_restarts,
        "unique_orders": metrics.get("unique_orders"),
        "unique_mock_payments": metrics.get("unique_mock_payments"),
        "duplicate_or_amount_currency_violations": 0,
        "audit_status": "PASS",
        "payment_provider": manifest.get("payment_provider"),
        "fulfillment_provider": manifest.get("fulfillment_provider"),
        "real_settlement": manifest.get("real_settlement"),
        "real_fulfillment": manifest.get("real_fulfillment"),
        "real_refund": manifest.get("real_refund"),
        "new_model_calls": manifest.get("new_model_calls"),
        "artifacts": evidence,
        "sha256": hashes,
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


def resolve_output_path(root: Path, requested: Path | None) -> Path:
    """Keep generated summaries inside the evidence root and never overwrite."""
    root_path = root.expanduser().resolve()
    if requested is None:
        output_path = root_path / "resume-safe-summary.json"
    else:
        requested_path = requested.expanduser()
        output_path = requested_path if requested_path.is_absolute() else root_path / requested_path
        output_path = output_path.resolve()
    try:
        output_path.relative_to(root_path)
    except ValueError as exc:
        raise SystemExit(f"FAIL / output must remain inside evidence root {root_path}: {output_path}") from exc
    if output_path.exists():
        raise SystemExit(f"FAIL / refusing to overwrite existing summary: {output_path}; choose a new --output name")
    return output_path


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
    parser.add_argument(
        "--output",
        type=Path,
        help="new summary filename inside --root (default: resume-safe-summary.json); existing files are never overwritten",
    )
    parser.add_argument(
        "--shopping-purchase-run",
        type=Path,
        help="optional explicit 300-order local shopping replay directory; never auto-selects a run",
    )
    args = parser.parse_args()
    root = args.root
    output_path = resolve_output_path(root, args.output)
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
    shopping_purchase: dict[str, Any] | None = None

    if args.shopping_purchase_run is not None:
        try:
            shopping_purchase = summarize_shopping_purchase_run(args.shopping_purchase_run)
        except ValueError as exc:
            raise SystemExit(f"FAIL / shopping purchase evidence rejected: {exc}") from exc
        run_ref = f".local-run/agent-shopping/lifecycle-evaluations/{shopping_purchase['run_id']}"
        claims.append({
            "claim": (
                f"Validated an isolated commerce purchase lifecycle across {shopping_purchase['attempts']} logical orders with "
                f"simulated payment/fulfillment: {shopping_purchase['settled']} mock settlements, "
                f"{shopping_purchase['declined']} deterministic declines, {shopping_purchase['delivered']} deliveries, "
                f"and {shopping_purchase['compensated_delivery_failures']} predeclared delivery failures compensated; "
                "response-loss/API-recreation/process-restart and concurrent duplicate replays produced no duplicate orders, "
                "payments, settlements, refunds, or audited state/amount mismatches."
            ),
            "situation": "A buyer may retry checkout after a lost response or a Runtime/API restart and accidentally create duplicate orders or payment effects.",
            "task": "Exercise purchase idempotency, persisted lifecycle state, deterministic payment failures, and fulfillment compensation across a fixed 300-order replay.",
            "action": "Replayed 100 response-loss retries, 100 retries after API recreation (including one OS process restart), and 100 concurrent duplicate-request pairs against an isolated MySQL tenant with simulation-only payment and fulfillment adapters; reconciled saved trials and SQL audit artifacts.",
            "result": f"{shopping_purchase['unique_orders']} unique orders and {shopping_purchase['unique_mock_payments']} unique mock payments; {shopping_purchase['settled']} settled, {shopping_purchase['declined']} declined, {shopping_purchase['delivered']} delivered, {shopping_purchase['compensated_delivery_failures']} predeclared delivery failures compensated; audit PASS with zero duplicate or amount/currency/state violations.",
            "evidence": shopping_purchase["artifacts"],
            "numerator": shopping_purchase,
            "limitations": [
                "Isolated local MySQL tenant with deterministic simulation-only payment, fulfillment, and refund; no real settlement, delivery, or refund.",
                "Replay audit reconciles saved evidence and is not a fresh live database query.",
                "This purchase lifecycle replay does not establish product-retrieval quality or the proposed 500-task shopping-quality rates.",
            ],
        })
    rejected.append({
        "experiment": "Shopping quality 500-task targets",
        "classification": "NOT_RESUME_ELIGIBLE",
        "resume_claim": False,
        "reason": "The available 500-task challenge is an auto-generated, unreviewed draft derived from the same synthetic catalog; historical feasible-SKU hit rate is not independently labeled target-product Recall@K, and evaluator-revision replay is not a controlled baseline/treatment experiment. The illustrative 94.2% recall and 72.6%→91.8% valid-plan rates therefore remain unverified.",
        "required_evidence": [
            "500 frozen tasks split before tuning, with independently authored or independently sourced target products and explicit relevance judgments.",
            "Two blinded reviewers plus adjudication for disputed labels; report Recall@K as relevant retrieved targets / all judged relevant targets, with task-level coverage and confidence interval.",
            "Define valid-plan rate as tasks with a feasible, policy-compliant plan / eligible PLAN tasks; freeze the policy and evaluator, then compare paired baseline and treatment on the same holdout with counts and uncertainty.",
        ],
    })

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
        "agent_shopping_purchase_replay": shopping_purchase,
        "limitations": ["This file is generated from existing evidence only.", "It does not edit docs/RESUME_VARIANTS.md.", "Missing, NOT RUN, or audit-pending experiments cannot support headline claims."],
    }
    output_path.parent.mkdir(parents=True, exist_ok=True)
    try:
        with output_path.open("x", encoding="utf-8", newline="\n") as stream:
            stream.write(json.dumps(output, ensure_ascii=False, indent=2) + "\n")
    except FileExistsError as exc:
        raise SystemExit(f"FAIL / refusing to overwrite existing summary: {output_path}; choose a new --output name") from exc
    print(output_path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
