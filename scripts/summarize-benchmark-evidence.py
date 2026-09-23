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


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".local-run/resume-benchmark", type=Path)
    args = parser.parse_args()
    root = args.root
    experiments = {name: load(root / name / "metrics.json") for name in ("e1-recovery", "e2-guard", "e3-load", "e4-chaos")}
    manifests = {name: load(root / name / "manifest.json") for name in experiments}
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
    if deepseek.get("status") == "RUN":
        deepseek_rate = deepseek.get("recovery_success", {}).get("rate")
        rule_rate = rule.get("recovery_success", {}).get("rate")
        if isinstance(deepseek_rate, (int, float)) and isinstance(rule_rate, (int, float)) and deepseek_rate > rule_rate:
            claims.append({
                "claim": "On the frozen E1 recovery dataset, the real DeepSeek treatment observed higher deterministic recovery success than the Rule control.",
                "evidence": ["e1-recovery/metrics.json", "e1-recovery/rule_results.jsonl", "e1-recovery/deepseek_results.jsonl", "e1-recovery/report.md"],
                "numerator": {"rule_recovery_success": rate(rule.get("recovery_success", {})), "deepseek_recovery_success": rate(deepseek.get("recovery_success", {}))},
                "limitations": ["Provider latency, token usage, guard rejection, and cost must be reviewed with the success rate; this is not a causal claim beyond the frozen local environment."]
            })
        else:
            rejected.append({"experiment": "E1", "reason": "DeepSeek ran but did not exceed the Rule control; NO POSITIVE RESUME CLAIM.", "evidence": ["e1-recovery/metrics.json", "e1-recovery/report.md"]})
    else:
        rejected.append({"experiment": "E1", "reason": "Real DeepSeek treatment was NOT RUN; no Rule-vs-DeepSeek claim is permitted.", "evidence": ["e1-recovery/manifest.json", "e1-recovery/metrics.json"]})

    e3 = experiments["e3-load"]
    if e3.get("status") == "COMPLETE":
        claims.append({
            "claim": "Observed Runtime HTTP/load results under deterministic local dependencies; request throughput and completed episode throughput remain separate metrics.",
            "evidence": ["e3-load/manifest.json", "e3-load/metrics.json", "e3-load/report.md"],
            "numerator": {"matrix": e3.get("matrix", [])},
            "limitations": ["A numeric resume claim requires inspecting each k6 summary and the duplicate-settlement/orphan audit."]
        })
    else:
        rejected.append({"experiment": "E3", "reason": "No completed load matrix is available; no QPS/latency claim.", "evidence": ["e3-load/manifest.json", "e3-load/metrics.json"]})

    e4 = experiments["e4-chaos"]
    if e4.get("correctness_status") == "PASS" and e4.get("resume_success", {}).get("denominator", 0) > 0:
        claims.append({
            "claim": "Crash/restart recovery and economic correctness passed the configured chaos audit.",
            "evidence": ["e4-chaos/manifest.json", "e4-chaos/trials.jsonl", "e4-chaos/metrics.json", "e4-chaos/report.md"],
            "numerator": {"resume_success": rate(e4.get("resume_success", {})), "ttr": e4.get("ttr_seconds")},
            "limitations": []
        })
    else:
        rejected.append({"experiment": "E4", "reason": "Chaos evidence is missing or its economic audit is pending; no resume claim.", "evidence": ["e4-chaos/manifest.json", "e4-chaos/metrics.json"]})

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
