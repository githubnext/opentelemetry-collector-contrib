import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("compare", Path(__file__).with_name("compare.py"))
compare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compare)


class CompareTests(unittest.TestCase):
    def test_metrics(self):
        result = compare.metrics(
            "BenchmarkTracePipeline-1 256 1200 ns/op 50 cpu-ns/span 256 spans/op 1 allocated-B/span\n")
        self.assertEqual(result["cpu-ns/span"], 50)
        self.assertEqual(result["spans/op"], 256)

    def test_invalid_observations(self):
        with self.assertRaises(ValueError):
            compare.metrics("PASS")
        with self.assertRaises(ValueError):
            compare.metrics("BenchmarkTracePipeline-1 256 1200 ns/op 0 cpu-ns/span")

    def test_census_excludes_timing(self):
        value = compare.census('TRACEBENCH_CENSUS={"counters":[{"Elapsed":100,"Calls":8}]}\n')
        self.assertEqual(value, {"counters": [{"Calls": 8}]})

    def test_interval_sign(self):
        result = compare.interval([-0.1] * 8)
        self.assertGreater(result["less_cpu_percent"], 0)
        self.assertEqual(result["conditional_ci95_percent"][0], result["conditional_ci95_percent"][1])

    def test_interval_rejects_degenerate_block_count(self):
        with self.assertRaisesRegex(ValueError, "At least eight"):
            compare.interval([-0.1, 0.2, -0.3, 0.4])

    def check_patch_context(self, context):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            base = (
                "func convert() {\n"
                "    preludeOne()\n"
                "    preludeTwo()\n"
                "    preludeThree()\n"
                "    chunks := newChunks()\n"
                "    containers := newContainers()\n"
                "    for span := range spans {\n"
                "        resource := resources[span]\n"
                "        handleResource(resource)\n"
                "        handleSpan(span)\n"
                "    }\n"
                "}\n")
            insertion = "    keys := newTagKeys()\n"
            head = base.replace("    for span := range spans {\n",
                                insertion + "    for span := range spans {\n")
            published = base.replace(
                "    preludeOne()\n    preludeTwo()\n    preludeThree()\n", "")
            for name, text in (("base", base), ("head", head), ("published", published)):
                path = root / name / "stats" / "convert.go"
                path.parent.mkdir(parents=True)
                path.write_text(text)
            patch = subprocess.run(
                ["git", "diff", "--no-index", f"-U{context}",
                 "base/stats/convert.go", "head/stats/convert.go"],
                cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            self.assertEqual(patch.returncode, 1, patch.stderr)
            patch_file = root / "proposal.patch"
            patch_file.write_text(patch.stdout)
            apply = lambda: compare.apply_source_patch(
                root / "published", patch_file, 2,
                dict(os.environ, GIT_CEILING_DIRECTORIES=str(root)), root / "apply.log")
            if context == 0:
                with self.assertRaisesRegex(ValueError, "surrounding context"):
                    apply()
            else:
                apply()
                self.assertEqual(
                    (root / "published/stats/convert.go").read_text(),
                    published.replace("    for span := range spans {\n",
                                      insertion + "    for span := range spans {\n"))

    def test_patch_follows_context_across_dependency_versions(self):
        self.check_patch_context(2)

    def test_zero_context_patch_is_rejected(self):
        self.check_patch_context(0)


if __name__ == "__main__":
    unittest.main()
