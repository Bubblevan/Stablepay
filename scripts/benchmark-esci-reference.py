#!/usr/bin/env python3
"""Prepare and score a deterministic, local-only Amazon ESCI test reference slice.

The public ESCI file contains query/product judgments, not live offers. This
script deliberately reports only ranking metrics over each query's judged
candidate set; it does not claim StablePay purchase-plan or payment quality.
"""

from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import math
import random
import re
import statistics
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable


REPO_ROOT = Path(__file__).resolve().parents[1]
LOCAL_REFERENCE_ROOT = (REPO_ROOT / ".local-run/agent-shopping/reference-data").resolve()
DEFAULT_SOURCE = LOCAL_REFERENCE_ROOT / "esci-us-test-20261011/esci-us-test.jsonl.gz"
PINNED_COMMIT = "13a365008a1a633310a1ff58992b789fcf526b0e"
EXPECTED_SOURCE_BYTES = 114_812_289
EXPECTED_SOURCE_SHA256 = "bb0db292e2185fea215bab11ef5d583aaf28eb1fc03cbcbca01a0c324009e11c"
MAX_SOURCE_BYTES = 150_000_000
LABEL_TO_GRADE = {"E": 3, "S": 2, "C": 1, "I": 0}
LABEL_TO_INT = {"E": 3, "S": 2, "C": 1, "I": 0}
TOKEN_RE = re.compile(r"[^\W_]+", re.UNICODE)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def checked_local_run_path(path: Path, label: str) -> Path:
    resolved = path.resolve()
    try:
        resolved.relative_to(LOCAL_REFERENCE_ROOT)
    except ValueError as exc:
        raise ValueError(f"{label} must remain under {LOCAL_REFERENCE_ROOT}") from exc
    return resolved


def iter_query_groups(source_path: Path) -> Iterable[list[dict[str, Any]]]:
    current_id: str | None = None
    current_query: str | None = None
    current: list[dict[str, Any]] = []
    closed_ids: set[str] = set()
    with gzip.open(source_path, "rt", encoding="utf-8") as source:
        for line_number, line in enumerate(source, 1):
            if not line.strip():
                continue
            try:
                row = json.loads(line)
            except json.JSONDecodeError as exc:
                raise ValueError(f"invalid JSON at source line {line_number}: {exc}") from exc
            if row.get("split") != "test":
                raise ValueError(f"non-test row at source line {line_number}")
            query_id = str(row.get("query_id", "")).strip()
            query = str(row.get("query", "")).strip()
            label = str(row.get("esci_label", "")).upper()
            if not query_id or not query or label not in LABEL_TO_GRADE:
                raise ValueError(f"missing query identity/text or invalid ESCI label at line {line_number}")
            if int(row.get("label", -1)) != LABEL_TO_INT[label]:
                raise ValueError(f"ESCI integer/letter label mismatch at line {line_number}")
            if current_id != query_id:
                if current:
                    closed_ids.add(current_id or "")
                    yield current
                if query_id in closed_ids:
                    raise ValueError(f"query_id {query_id!r} is not contiguous in the pinned test split")
                current_id, current_query, current = query_id, query, []
            elif query != current_query:
                raise ValueError(f"query text changed inside query_id {query_id!r}")
            current.append(row)
    if current:
        yield current


def candidate_from_row(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "judgment_id": str(row.get("example_id", row.get("id", ""))),
        "product_id": str(row["product_id"]),
        "label": str(row["esci_label"]).upper(),
        "title": str(row.get("product_title") or ""),
        "brand": str(row.get("product_brand") or ""),
        "color": str(row.get("product_color") or ""),
        "bullet_point": str(row.get("product_bullet_point") or ""),
        "description": str(row.get("product_description") or ""),
    }


def dedupe_group(rows: list[dict[str, Any]]) -> tuple[dict[str, Any], int]:
    first = rows[0]
    candidates: dict[str, dict[str, Any]] = {}
    removed = 0
    for row in rows:
        candidate = candidate_from_row(row)
        product_id = candidate["product_id"]
        previous = candidates.get(product_id)
        if previous is not None:
            if previous["label"] != candidate["label"]:
                raise ValueError(f"conflicting labels for product {product_id} in query {first['query_id']}")
            removed += 1
            continue
        candidates[product_id] = candidate
    return {
        "query_id": str(first["query_id"]),
        "query": str(first["query"]),
        "split": "test",
        "candidates": list(candidates.values()),
    }, removed


def percentile(values: list[float], quantile: float) -> float:
    ordered = sorted(values)
    index = max(0, min(len(ordered) - 1, math.ceil(quantile * len(ordered)) - 1))
    return ordered[index]


def bootstrap_ci(values: list[float], seed: int, iterations: int = 2000) -> list[float]:
    rng = random.Random(seed)
    means = [statistics.fmean(rng.choices(values, k=len(values))) for _ in range(iterations)]
    return [round(percentile(means, 0.025), 6), round(percentile(means, 0.975), 6)]


def tokenize(text: str) -> list[str]:
    return [token.casefold() for token in TOKEN_RE.findall(text) if token]


def title_overlap_scores(query: str, candidates: list[dict[str, Any]]) -> list[tuple[float, str]]:
    query_terms = set(tokenize(query))
    denominator = max(1, len(query_terms))
    scored = []
    for candidate in candidates:
        title_terms = set(tokenize(candidate["title"]))
        score = len(query_terms & title_terms) / denominator
        scored.append((score, candidate["product_id"]))
    return sorted(scored, key=lambda item: (-item[0], item[1]))


def bm25_scores(query: str, candidates: list[dict[str, Any]]) -> list[tuple[float, str]]:
    documents = [
        tokenize(" ".join((c["title"], c["brand"], c["color"], c["bullet_point"], c["description"])))
        for c in candidates
    ]
    query_terms = set(tokenize(query))
    if not documents:
        return []
    count = len(documents)
    lengths = [len(document) for document in documents]
    average_length = max(1.0, statistics.fmean(lengths))
    document_frequency: dict[str, int] = {}
    for document in documents:
        for term in set(document):
            document_frequency[term] = document_frequency.get(term, 0) + 1
    scores: list[tuple[float, str]] = []
    k1, b = 1.2, 0.75
    for candidate, document, length in zip(candidates, documents, lengths):
        frequencies: dict[str, int] = {}
        for term in document:
            frequencies[term] = frequencies.get(term, 0) + 1
        score = 0.0
        for term in query_terms:
            tf = frequencies.get(term, 0)
            if not tf:
                continue
            df = document_frequency.get(term, 0)
            inverse = math.log(1.0 + (count - df + 0.5) / (df + 0.5))
            denominator = tf + k1 * (1.0 - b + b * length / average_length)
            score += inverse * tf * (k1 + 1.0) / denominator
        scores.append((score, candidate["product_id"]))
    return sorted(scores, key=lambda item: (-item[0], item[1]))


def score_ranking(groups: list[dict[str, Any]], ranker: str, cutoff: int) -> dict[str, Any]:
    per_task_recall: list[float] = []
    per_task_hit: list[float] = []
    per_task_ndcg: list[float] = []
    total_relevant = 0
    total_retrieved = 0
    total_candidates = 0
    label_counts = {label: 0 for label in LABEL_TO_GRADE}
    for group in groups:
        candidates = group["candidates"]
        total_candidates += len(candidates)
        for candidate in candidates:
            label_counts[candidate["label"]] += 1
        relevance = {c["product_id"]: c["label"] for c in candidates}
        exact = {sku for sku, label in relevance.items() if label == "E"}
        if not exact:
            raise ValueError(f"sampled query {group['query_id']} has no exact target")
        ranking = title_overlap_scores(group["query"], candidates) if ranker == "title_overlap" else bm25_scores(group["query"], candidates)
        top = [sku for _, sku in ranking[:cutoff]]
        hits = len(set(top) & exact)
        total_relevant += len(exact)
        total_retrieved += hits
        per_task_recall.append(hits / len(exact))
        per_task_hit.append(float(hits > 0))
        gains = [float((2 ** LABEL_TO_GRADE[relevance[sku]]) - 1) for sku in top]
        dcg = sum(gain / math.log2(index + 2) for index, gain in enumerate(gains))
        ideal = sorted((2 ** LABEL_TO_GRADE[label] - 1 for label in relevance.values()), reverse=True)[:cutoff]
        idcg = sum(gain / math.log2(index + 2) for index, gain in enumerate(ideal))
        per_task_ndcg.append(dcg / idcg if idcg else 0.0)

    suffix = 20 if cutoff == 20 else cutoff
    return {
        "ranker": ranker,
        "cutoff": cutoff,
        "task_count": len(groups),
        "candidate_count": total_candidates,
        "mean_candidates_per_task": round(total_candidates / max(1, len(groups)), 4),
        "label_pair_counts": label_counts,
        "exact_target_recall_macro": round(statistics.fmean(per_task_recall), 6),
        "exact_target_recall_macro_ci95_task_bootstrap": bootstrap_ci(per_task_recall, 20261010 + suffix),
        "exact_target_recall_micro": round(total_retrieved / total_relevant, 6),
        "exact_target_task_hit_rate": round(statistics.fmean(per_task_hit), 6),
        "exact_target_task_hit_rate_ci95_task_bootstrap": bootstrap_ci(per_task_hit, 20301010 + suffix),
        "esci_ndcg_macro": round(statistics.fmean(per_task_ndcg), 6),
        "esci_ndcg_macro_ci95_task_bootstrap": bootstrap_ci(per_task_ndcg, 20401010 + suffix),
        "exact_targets": total_relevant,
        "exact_targets_retrieved": total_retrieved,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=DEFAULT_SOURCE)
    parser.add_argument("--out-dir", type=Path, default=DEFAULT_SOURCE.parent)
    parser.add_argument(
        "--exclude-sample",
        type=Path,
        action="append",
        default=[],
        help="optional prior JSONL sample under .local-run/reference-data; excluded query IDs will not be sampled again",
    )
    parser.add_argument("--queries", type=int, default=500)
    parser.add_argument("--seed", type=int, default=20261010)
    parser.add_argument("--cutoff", type=int, default=20)
    parser.add_argument(
        "--profile",
        choices=("uniform_exact", "sparse_exact"),
        default="uniform_exact",
        help="uniformly sample all queries with Exact labels, or emphasize queries with few Exact products",
    )
    args = parser.parse_args()

    source = checked_local_run_path(args.source, "source")
    out_dir = checked_local_run_path(args.out_dir, "output directory")
    if not source.is_file():
        raise FileNotFoundError(source)
    size = source.stat().st_size
    if size > MAX_SOURCE_BYTES or size != EXPECTED_SOURCE_BYTES:
        raise ValueError(f"pinned source size is {size} bytes; expected {EXPECTED_SOURCE_BYTES} and maximum is {MAX_SOURCE_BYTES}")
    source_digest = sha256_file(source)
    if source_digest != EXPECTED_SOURCE_SHA256:
        raise ValueError(f"pinned source SHA-256 mismatch: {source_digest}")
    if not 1 <= args.queries <= 500 or not 1 <= args.cutoff <= 100:
        raise ValueError("queries must be in [1,500] and cutoff in [1,100]")
    excluded_query_ids: set[str] = set()
    excluded_samples: list[dict[str, Any]] = []
    if len(args.exclude_sample) > 20:
        raise ValueError("at most 20 prior samples may be excluded in one run")
    for excluded_argument in args.exclude_sample:
        excluded_sample_path = checked_local_run_path(excluded_argument, "excluded sample")
        if not excluded_sample_path.is_file() or excluded_sample_path.stat().st_size > 20 * 1024 * 1024:
            raise ValueError("excluded sample must be an existing JSONL file no larger than 20 MiB")
        excluded_sample_digest = sha256_file(excluded_sample_path)
        sample_query_ids: set[str] = set()
        with excluded_sample_path.open("r", encoding="utf-8") as excluded_source:
            for line_number, line in enumerate(excluded_source, 1):
                if not line.strip():
                    raise ValueError(f"blank row in excluded sample at line {line_number}")
                try:
                    group = json.loads(line)
                except json.JSONDecodeError as exc:
                    raise ValueError(f"invalid JSON in excluded sample at line {line_number}: {exc}") from exc
                query_id = str(group.get("query_id", "")).strip()
                if not query_id or query_id in sample_query_ids:
                    raise ValueError(f"missing or duplicate query_id in excluded sample at line {line_number}")
                sample_query_ids.add(query_id)
        overlap = excluded_query_ids.intersection(sample_query_ids)
        if overlap:
            raise ValueError(f"prior excluded samples overlap on {len(overlap)} query IDs")
        excluded_query_ids.update(sample_query_ids)
        excluded_samples.append({
            "path": excluded_sample_path.relative_to(LOCAL_REFERENCE_ROOT).as_posix(),
            "sha256": f"sha256:{excluded_sample_digest}",
            "query_ids": len(sample_query_ids),
        })
    out_dir.mkdir(parents=True, exist_ok=True)
    sample_path = out_dir / f"esci-us-test-{args.queries}-queries.jsonl"
    metrics_path = out_dir / "reference-ranking-metrics.json"
    manifest_path = out_dir / "reference-sample-manifest.json"
    for path in (sample_path, metrics_path, manifest_path):
        if path.exists():
            raise FileExistsError(f"refusing to overwrite existing artifact: {path}")

    rng = random.Random(args.seed)
    sampled: list[dict[str, Any]] = []
    single_exact_groups: list[dict[str, Any]] = []
    two_to_five_exact_groups: list[dict[str, Any]] = []
    scanned_groups = 0
    eligible_groups = 0
    single_exact_groups_seen = 0
    two_to_five_exact_groups_seen = 0
    six_plus_exact_groups_seen = 0
    source_rows = 0
    excluded_source_groups = 0
    duplicate_pairs_removed = 0
    for rows in iter_query_groups(source):
        scanned_groups += 1
        source_rows += len(rows)
        if str(rows[0].get("query_id", "")).strip() in excluded_query_ids:
            excluded_source_groups += 1
            continue
        group, removed = dedupe_group(rows)
        duplicate_pairs_removed += removed
        exact_count = sum(candidate["label"] == "E" for candidate in group["candidates"])
        if exact_count == 0:
            continue
        eligible_groups += 1
        if exact_count == 1:
            single_exact_groups_seen += 1
        elif exact_count <= 5:
            two_to_five_exact_groups_seen += 1
        else:
            six_plus_exact_groups_seen += 1

        if args.profile == "uniform_exact":
            if len(sampled) < args.queries:
                sampled.append(group)
            else:
                position = rng.randrange(eligible_groups)
                if position < args.queries:
                    sampled[position] = group
        elif exact_count == 1:
            single_exact_groups.append(group)
        elif exact_count <= 5:
            two_to_five_exact_groups.append(group)

    if excluded_samples and excluded_source_groups != len(excluded_query_ids):
        raise ValueError(
            "excluded sample query IDs do not all resolve in the pinned source: "
            f"matched={excluded_source_groups}, excluded={len(excluded_query_ids)}"
        )

    if args.profile == "sparse_exact":
        if len(single_exact_groups) >= args.queries:
            sampled = rng.sample(single_exact_groups, args.queries)
        else:
            remaining = args.queries - len(single_exact_groups)
            if len(two_to_five_exact_groups) < remaining:
                raise ValueError(
                    "not enough sparse-Exact query groups to fill the requested sample: "
                    f"single-Exact={len(single_exact_groups)}, "
                    f"two-to-five-Exact={len(two_to_five_exact_groups)}, requested={args.queries}"
                )
            sampled = single_exact_groups + rng.sample(two_to_five_exact_groups, remaining)
    if len(sampled) != args.queries:
        raise ValueError(f"only {len(sampled)} exact-target query groups found; requested {args.queries}")
    sampled.sort(key=lambda group: (int(group["query_id"]) if group["query_id"].isdigit() else 0, group["query_id"]))

    with sample_path.open("x", encoding="utf-8", newline="\n") as destination:
        for group in sampled:
            destination.write(json.dumps(group, ensure_ascii=False, separators=(",", ":")) + "\n")
    metrics = {
        "schema_version": "stablepay.esci-reference-ranking.v1",
        "dataset_role": "PUBLIC_REFERENCE_ONLY_NOT_STABLEPAY_RESULT",
        "task_count": len(sampled),
        "sampling": {
            "profile": args.profile,
            "method": (
                "uniform reservoir sample over complete test queries with >=1 Exact judgment"
                if args.profile == "uniform_exact"
                else "include all single-Exact queries when fewer than requested; fill remainder uniformly from queries with 2-5 Exact judgments; exclude queries with 6+ Exact judgments"
            ),
            "seed": args.seed,
            "source_exact_count_strata": {
                "one_exact": single_exact_groups_seen,
                "two_to_five_exact": two_to_five_exact_groups_seen,
                "six_plus_exact": six_plus_exact_groups_seen,
            },
            "sample_exact_count_strata": {
                "one_exact": sum(sum(c["label"] == "E" for c in group["candidates"]) == 1 for group in sampled),
                "two_to_five_exact": sum(2 <= sum(c["label"] == "E" for c in group["candidates"]) <= 5 for group in sampled),
                "six_plus_exact": sum(sum(c["label"] == "E" for c in group["candidates"]) >= 6 for group in sampled),
            },
        },
        "judged_candidate_ranking": {
            "candidate_count": sum(len(group["candidates"]) for group in sampled),
            "mean_candidates_per_query": round(statistics.fmean(len(group["candidates"]) for group in sampled), 4),
            "candidate_set_boundary": "ESCI judged products for each query only; no unjudged catalog negatives or broad-market retrieval recall",
            "target_relevance": "Exact (E) only; Substitute (S), Complement (C), Irrelevant (I) do not count as exact target recall",
            "ndcg_gain": {"E": 7, "S": 3, "C": 1, "I": 0},
            "cutoff": args.cutoff,
            "rankers": [
                score_ranking(sampled, "title_overlap", args.cutoff),
                score_ranking(sampled, "bm25_title_and_product_text", args.cutoff),
            ],
        },
        "purchase_plan_rate": {"status": "NOT_MEASURED_NO_PRICE_STOCK_REGION_BUDGET_OR_BUYER_CONSENT_LABELS"},
        "payment_or_fulfillment": {"status": "NOT_MEASURED"},
    }
    metrics_bytes = (json.dumps(metrics, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
    metrics_path.write_bytes(metrics_bytes)
    manifest = {
        "schema_version": "stablepay.esci-reference-sample-manifest.v2",
        "created_at_utc": datetime.now(timezone.utc).isoformat(),
        "dataset": "Amazon Shopping Queries Dataset (ESCI), third-party US test-split preparation",
        "source_repository": "https://huggingface.co/datasets/shuttie/esci-us-small",
        "source_revision": PINNED_COMMIT,
        "source_file": f"data/test.jsonl.gz ({EXPECTED_SOURCE_BYTES} bytes)",
        "source_sha256": f"sha256:{source_digest}",
        "source_split": "test",
        "mirror_license_claim": "Apache-2.0 on the Hugging Face dataset card; original upstream terms should be checked before any redistribution.",
        "usage_boundary": "local benchmark reference only; do not redistribute the source file or present these metrics as StablePay production or transaction results",
        "source_rows_scanned": source_rows,
        "source_query_groups_scanned": scanned_groups,
        "excluded_samples": [
            {**sample, "matching_source_query_groups": sample["query_ids"]}
            for sample in excluded_samples
        ],
        "excluded_query_id_total": len(excluded_query_ids),
        "excluded_query_ids_matching_source": excluded_source_groups,
        "source_exact_target_query_groups": eligible_groups,
        "source_exact_count_strata": {
            "one_exact": single_exact_groups_seen,
            "two_to_five_exact": two_to_five_exact_groups_seen,
            "six_plus_exact": six_plus_exact_groups_seen,
        },
        "sampling_profile": args.profile,
        "sample_exact_count_strata": metrics["sampling"]["sample_exact_count_strata"],
        "sample_query_count": len(sampled),
        "sample_candidate_pair_count": metrics["judged_candidate_ranking"]["candidate_count"],
        "duplicate_product_pairs_removed_within_query": duplicate_pairs_removed,
        "sampling_seed": args.seed,
        "sample_sha256": f"sha256:{sha256_file(sample_path)}",
        "metrics_sha256": f"sha256:{hashlib.sha256(metrics_bytes).hexdigest()}",
        "purchase_plan_and_payment_metrics": "NOT_MEASURED",
    }
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"manifest": str(manifest_path), "sample": str(sample_path), "metrics": str(metrics_path),
                      "sample_queries": len(sampled), "source_queries_scanned": scanned_groups,
                      "candidate_pairs": metrics["judged_candidate_ranking"]["candidate_count"],
                      "metrics": metrics["judged_candidate_ranking"]["rankers"]}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
