import importlib.util
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("summarize-benchmark-evidence.py")
SPEC = importlib.util.spec_from_file_location("summarize_benchmark_evidence", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)
FAKE_ROOT = Path(__file__).resolve().parents[1] / ".local-run" / "resume-benchmark"


class EvidencePathTests(unittest.TestCase):
    def test_resolves_explicit_run_directory_to_full(self):
        run = Path("D:/fake/e4-local-run")
        full = run / "full"
        with patch.object(Path, "is_file", lambda path: path.name in {"metrics.json", "audit-summary.json"}):
            self.assertEqual(MODULE.resolve_e4_full_run(run), full.resolve())

    def test_accepts_explicit_full_directory(self):
        full = Path("D:/fake/e4-local-run/full")
        with patch.object(Path, "is_file", lambda path: path.name in {"metrics.json", "audit-summary.json"}):
            self.assertEqual(MODULE.resolve_e4_full_run(full), full.resolve())

    def test_rejects_e4_run_without_matching_audit(self):
        run = Path("D:/fake/e4-local-run")
        with patch.object(Path, "is_file", lambda path: path.name == "metrics.json"):
            with self.assertRaises(SystemExit):
                MODULE.resolve_e4_full_run(run)

    def test_output_defaults_inside_root_and_refuses_overwrite(self):
        with patch.object(Path, "exists", lambda path: False):
            default = MODULE.resolve_output_path(FAKE_ROOT, None)
        self.assertEqual(default, FAKE_ROOT.resolve() / "resume-safe-summary.json")
        with patch.object(Path, "exists", lambda path: path.name == "resume-safe-summary.json"):
            with self.assertRaises(SystemExit):
                MODULE.resolve_output_path(FAKE_ROOT, None)

    def test_output_rejects_escape_from_evidence_root(self):
        with self.assertRaises(SystemExit):
            MODULE.resolve_output_path(FAKE_ROOT, Path("..") / "outside.json")

    def test_shopping_run_must_be_under_local_evidence_root(self):
        with self.assertRaisesRegex(ValueError, "must be below"):
            MODULE.resolve_shopping_purchase_run(SCRIPT.parent)

    def test_shopping_run_requires_all_four_saved_artifacts(self):
        run = SCRIPT.resolve().parents[1] / ".local-run" / "agent-shopping" / "lifecycle-evaluations" / "missing-test-run"
        with patch.object(Path, "is_file", lambda path: False):
            with self.assertRaisesRegex(ValueError, "manifest.json, metrics.json, audit-summary.json, trials.jsonl"):
                MODULE.resolve_shopping_purchase_run(run)


if __name__ == "__main__":
    unittest.main()
