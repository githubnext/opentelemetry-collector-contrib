import json
import unittest

from github_workload import VERSION, workload
import compare


class GitHubWorkloadTests(unittest.TestCase):
    def test_shared_runner_and_workload_version(self):
        self.assertEqual(VERSION, "1.0.0")
        self.assertEqual(compare.VERSION, VERSION)

    def test_self_contained_synthetic_workload(self):
        spec = workload()
        self.assertEqual(spec, json.loads(json.dumps(spec)))
        self.assertEqual(spec["bank"], 96)
        self.assertAlmostEqual(sum(c["weight"] for c in spec["classes"]), 1)
        self.assertAlmostEqual(sum(b["weight"] for b in spec["batches"]), 1)
        self.assertEqual({c["kind"] for c in spec["classes"]}, {1, 2, 3, 4, 5})
        for cls in spec["classes"]:
            self.assertEqual(len(cls["resource"]), 40)
            self.assertEqual(sum(k.startswith("synthetic.resource.") for k in cls["resource"]), 30)
            self.assertEqual(cls["spans_per_resource"], 128)
        for stage in spec["common"] + spec["sampled"]:
            self.assertIn("config", stage)
            self.assertNotIn("file", stage)
        self.assertEqual(len(spec["common"][0]["config"]["trace_statements"][0]["statements"]), 11)
        self.assertEqual(spec["sample_percent"], 1)
        self.assertTrue(spec["stats"])


if __name__ == "__main__":
    unittest.main()
