#!/usr/bin/env python3
"""Read-only exact-title compatibility audit for the local ShoppingComp snapshot."""

from __future__ import annotations

import argparse
import hashlib
import json
import sqlite3
import sys
import unicodedata
from pathlib import Path
from typing import Any


DEFAULT_DATASET_RELATIVE = Path(
    ".local-run/agent-shopping/reference-data/shoppingcomp-201dd305"
)
DEFAULT_CATALOG_RELATIVE = Path(".local-run/simulated-merchants")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def normalize_title(value: str) -> str:
    normalized = unicodedata.normalize("NFKC", value).casefold()
    return "".join(character for character in normalized if character.isalnum())


def load_verified_manifest(dataset_dir: Path) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    manifest_path = dataset_dir / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    files = manifest.get("files")
    if not isinstance(files, list) or not files:
        raise ValueError(f"manifest has no file entries: {manifest_path}")

    task_files: list[dict[str, Any]] = []
    actual_total = 0
    for entry in files:
        name = entry.get("name")
        expected_bytes = entry.get("bytes")
        expected_hash = entry.get("sha256")
        if not isinstance(name, str) or not isinstance(expected_bytes, int) or not isinstance(expected_hash, str):
            raise ValueError(f"invalid manifest file entry: {entry!r}")
        path = dataset_dir / name
        if not path.is_file():
            raise FileNotFoundError(path)
        actual_bytes = path.stat().st_size
        actual_hash = sha256_file(path)
        if actual_bytes != expected_bytes or actual_hash != expected_hash:
            raise ValueError(f"dataset file does not match manifest: {name}")
        actual_total += actual_bytes
        if "expert-grounded tasks" in entry.get("role", ""):
            task_files.append(entry)

    if actual_total != manifest.get("aggregate_bytes"):
        raise ValueError(
            f"dataset aggregate mismatch: manifest={manifest.get('aggregate_bytes')} actual={actual_total}"
        )
    if not task_files:
        raise ValueError("manifest contains no expert-grounded task files")
    return manifest, task_files


def load_active_catalog_titles(catalog_root: Path) -> tuple[set[str], int, int]:
    databases = sorted(catalog_root.glob("sim-m*/data/merchant.db"))
    if not databases:
        raise FileNotFoundError(f"no simulated Merchant SQLite databases under {catalog_root}")

    normalized_titles: set[str] = set()
    active_rows = 0
    for database in databases:
        uri = "file:" + database.resolve().as_posix() + "?mode=ro"
        connection = sqlite3.connect(uri, uri=True)
        try:
            connection.execute("PRAGMA query_only=ON")
            cursor = connection.execute(
                "SELECT title FROM products WHERE status = 'active'"
            )
            for (title,) in cursor:
                if isinstance(title, str) and title.strip():
                    active_rows += 1
                    normalized_titles.add(normalize_title(title))
        finally:
            connection.close()
    return normalized_titles, active_rows, len(databases)


def audit_task_file(dataset_dir: Path, entry: dict[str, Any], catalog_titles: set[str]) -> dict[str, Any]:
    path = dataset_dir / entry["name"]
    task_count = 0
    gold_reference_count = 0
    matched_reference_count = 0
    tasks_with_match = 0

    with path.open("r", encoding="utf-8") as handle:
        for line_number, line in enumerate(handle, start=1):
            if not line.strip():
                continue
            try:
                task = json.loads(line)
            except json.JSONDecodeError as error:
                raise ValueError(f"invalid JSONL at {path}:{line_number}: {error}") from error
            task_count += 1
            references = [
                product.get("product_name")
                for product in task.get("product_list", [])
                if isinstance(product, dict)
                and isinstance(product.get("product_name"), str)
                and product["product_name"].strip()
            ]
            gold_reference_count += len(references)
            task_hit = False
            for reference in references:
                if normalize_title(reference) in catalog_titles:
                    matched_reference_count += 1
                    task_hit = True
            tasks_with_match += int(task_hit)

    if task_count != entry.get("rows"):
        raise ValueError(
            f"task row count mismatch for {entry['name']}: manifest={entry.get('rows')} actual={task_count}"
        )
    return {
        "file": entry["name"],
        "tasks": task_count,
        "gold_product_references": gold_reference_count,
        "tasks_with_exact_title_match": tasks_with_match,
        "gold_references_with_exact_title_match": matched_reference_count,
    }


def main() -> int:
    repo_root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--dataset-dir",
        type=Path,
        default=repo_root / DEFAULT_DATASET_RELATIVE,
        help="directory containing the pinned JSONL files and manifest.json",
    )
    parser.add_argument(
        "--catalog-root",
        type=Path,
        default=repo_root / DEFAULT_CATALOG_RELATIVE,
        help="root containing sim-m*/data/merchant.db files",
    )
    args = parser.parse_args()

    try:
        dataset_dir = args.dataset_dir.resolve(strict=True)
        catalog_root = args.catalog_root.resolve(strict=True)
        manifest, task_files = load_verified_manifest(dataset_dir)
        titles, active_rows, database_count = load_active_catalog_titles(catalog_root)
        reports = [audit_task_file(dataset_dir, entry, titles) for entry in task_files]
        total_tasks = sum(report["tasks"] for report in reports)
        total_references = sum(report["gold_product_references"] for report in reports)
        total_task_hits = sum(report["tasks_with_exact_title_match"] for report in reports)
        total_reference_hits = sum(
            report["gold_references_with_exact_title_match"] for report in reports
        )
        result = {
            "audit_type": "EXACT_TITLE_COMPATIBILITY_ONLY_NOT_RECALL",
            "dataset": manifest.get("dataset"),
            "dataset_revision": manifest.get("revision"),
            "dataset_aggregate_bytes_verified": manifest.get("aggregate_bytes"),
            "catalog_database_count": database_count,
            "catalog_active_product_rows": active_rows,
            "catalog_unique_normalized_titles": len(titles),
            "normalization": "Unicode NFKC, casefold, retain alphanumeric characters, exact equality",
            "task_files": reports,
            "totals": {
                "tasks": total_tasks,
                "gold_product_references": total_references,
                "tasks_with_exact_title_match": total_task_hits,
                "gold_references_with_exact_title_match": total_reference_hits,
            },
            "interpretation": (
                "A zero exact-title match indicates this snapshot cannot directly score these gold products "
                "against the current catalog; it is not a semantic retrieval-recall result."
            ),
        }
        print(json.dumps(result, ensure_ascii=True, indent=2))
        return 0
    except (OSError, sqlite3.Error, ValueError, json.JSONDecodeError) as error:
        print(f"audit failed: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
