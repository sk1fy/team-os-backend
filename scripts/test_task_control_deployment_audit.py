import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location("audit", Path(__file__).with_name("task-control-deployment-audit.py"))
audit = importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)


class DeploymentAuditTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.archive = Path(self.temp.name) / "widget.zip"
        with zipfile.ZipFile(self.archive, "w") as package:
            package.writestr("manifest.json", json.dumps({"widget": {"version": "1.2.3"}}))
            package.writestr("lib/deployment.js", 'define([], function () { return ' + json.dumps({"coreOrigin": "https://core.example.com", "backendOrigin": "https://backend.example.com"}) + '; });\n')
        image = {"localImageId": "sha256:" + "b" * 64, "revision": "a" * 40, "architecture": "amd64"}
        self.candidate = {
            "localBuildPassed": True, "imagesComplete": True, "runtimeVersion": "1.2.3",
            "repositories": {repo: {"sha": "a" * 40} for repo in ["core", "backend"]},
            "images": {key: dict(image) for key in audit.COMPONENTS},
            "archives": {"managed": {"sha256": hashlib.sha256(self.archive.read_bytes()).hexdigest()}},
            "publicConfiguration": {"coreOrigin": "https://core.example.com", "backendOrigin": "https://backend.example.com", "oauthClientUUID": "67bf529d-d3ef-4b89-b68a-739da9188a19"},
        }
        self.observed = {key: {**image, "running": True} for key in audit.COMPONENTS}

    def compare(self):
        return audit.compare(self.candidate, self.observed, self.archive, "managed")

    def test_matching_artifacts_never_certify_live_acceptance(self):
        result = self.compare()
        self.assertTrue(result["artifactMatch"])
        self.assertFalse(result["acceptancePassed"])
        self.assertFalse(result["releaseApproved"])

    def test_old_or_unattributed_image_and_stopped_container_fail(self):
        for field, value in [("revision", None), ("revision", "c" * 40), ("localImageId", "sha256:old"), ("architecture", "arm64"), ("running", False)]:
            with self.subTest(field=field, value=value):
                observed = copy.deepcopy(self.observed)
                self.observed["backend/gateway"][field] = value
                self.assertFalse(self.compare()["artifactMatch"])
                self.observed = observed

    def test_manifest_cannot_relabel_another_source(self):
        self.candidate["repositories"]["backend"]["sha"] = "d" * 40
        self.assertFalse(self.compare()["artifactMatch"])

    def test_other_zip_and_wrong_runtime_are_rejected(self):
        self.candidate["archives"]["managed"]["sha256"] = "wrong"
        self.candidate["runtimeVersion"] = "1.1.2"
        self.assertEqual(len(self.compare()["errors"]), 2)

    def test_synthetic_candidate_cannot_pass_managed_audit(self):
        self.candidate["publicConfiguration"]["coreOrigin"] = "https://core.example.invalid"
        self.candidate["publicConfiguration"]["oauthClientUUID"] = None
        self.assertIn("coreOrigin: нет пригодного HTTPS origin", self.compare()["errors"])
        self.assertFalse(self.compare()["artifactMatch"])

    def test_zip_configuration_must_match_even_with_correct_checksum(self):
        self.candidate["publicConfiguration"]["coreOrigin"] = "https://core.example.com:8443"
        result = self.compare()
        self.assertFalse(result["artifactMatch"])
        self.assertIn("Origins внутри ZIP не совпадают с режимом и candidate.json", result["errors"])

    def test_managed_origins_cannot_pass_as_standalone(self):
        self.candidate["archives"]["standalone"] = self.candidate["archives"]["managed"]
        self.assertFalse(audit.compare(self.candidate, self.observed, self.archive, "standalone")["artifactMatch"])

    def test_incomplete_build_is_rejected(self):
        self.candidate["imagesComplete"] = False
        self.assertFalse(self.compare()["artifactMatch"])


if __name__ == "__main__":
    unittest.main()
