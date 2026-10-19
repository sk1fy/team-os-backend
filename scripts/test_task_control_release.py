"""CLI test with disposable Git repos and a fake Docker executable; no daemon."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class ReleaseTest(unittest.TestCase):
    def test_rebuild_keeps_previous_tags_and_archives_exact_ids(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            backend, core, widgets = [root / name for name in ["backend", "core", "widgets"]]
            (backend / "scripts").mkdir(parents=True)
            source = Path(__file__).with_name("task-control-release.py")
            builder = backend / "scripts/task-control-release.py"
            builder.write_bytes(source.read_bytes())
            core.mkdir()
            (core / "README").write_text("fixture")
            widget = widgets / "rkrs_lead_notasks"
            (widget / "scripts").mkdir(parents=True)
            (widget / "manifest.json").write_text('{"widget":{"version":"1.2.4"}}')
            (widget / "scripts/build.sh").write_text('''python3 - <<'PY'
import json, os, zipfile
with zipfile.ZipFile(os.environ['TASK_CONTROL_OUTPUT'], 'w') as z:
    z.writestr(zipfile.ZipInfo('manifest.json'), open('manifest.json').read())
    z.writestr(zipfile.ZipInfo('lib/deployment.js'), 'define([], function () { return ' + json.dumps({k:os.environ.get(v,'') for k,v in [('coreOrigin','TASK_CONTROL_CORE_ORIGIN'),('backendOrigin','TASK_CONTROL_BACKEND_ORIGIN')]}) + '; });')
PY
''')
            for repo in [backend, core, widgets]:
                for command in [["init", "-q"], ["add", "."], ["-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture"]]:
                    subprocess.run(["git", *command], cwd=repo, check=True, capture_output=True)
            executables = root / "bin"
            executables.mkdir()
            docker = executables / "docker"
            docker.write_text('''#!/usr/bin/env python3
import hashlib, json, os, pathlib, sys
args = sys.argv[1:]
state = pathlib.Path(os.environ['FAKE_DOCKER_STATE'])
images = json.loads(state.read_text()) if state.exists() else {}
if args[0] == 'build':
    tag = args[args.index('-t')+1]
    revision = args[args.index('--build-arg')+1].split('=',1)[1]
    images[tag] = {'id':'sha256:'+hashlib.sha256(tag.encode()).hexdigest(),'revision':revision}
    state.write_text(json.dumps(images))
elif args[:2] == ['image','inspect']:
    image = images[args[-1]]
    for value in [image['id'],[],{'org.opencontainers.image.revision':image['revision']},'amd64']:
        print(json.dumps(value))
elif args[:2] == ['image','save']:
    image_id = args[-1]
    assert image_id in [i['id'] for i in images.values()], 'must archive a recorded ID'
    pathlib.Path(args[args.index('--output')+1]).write_bytes(image_id.encode())
else:
    raise SystemExit('unexpected docker command')
''')
            docker.chmod(0o700)
            state = root / "docker.json"
            env = {**os.environ, "PATH": str(executables) + os.pathsep + os.environ["PATH"], "FAKE_DOCKER_STATE": str(state)}
            candidates = []
            for index in range(2):
                output = root / f"candidate-{index}"
                subprocess.run(["python3", str(builder), "--core", str(core), "--widgets", str(widgets), "--output", str(output), "--build-images"], env=env, check=True, capture_output=True)
                candidate = json.loads((output / "candidate.json").read_text())
                self.assertEqual(candidate["runtimeVersion"], "1.2.4")
                self.assertTrue(candidate["imagesComplete"])
                self.assertFalse(candidate["releaseApproved"])
                self.assertEqual(len(candidate["images"]), 8)
                for image in candidate["images"].values():
                    data = Path(image["archive"]["path"]).read_bytes()
                    self.assertEqual(data.decode(), image["localImageId"])
                    self.assertEqual(hashlib.sha256(data).hexdigest(), image["archive"]["sha256"])
                for repo in ["core", "backend"]:
                    override = json.loads(Path(candidate["composeOverrides"][repo]).read_text())
                    for service, config in override["services"].items():
                        self.assertEqual(config["image"], candidate["images"][repo + "/" + service]["localImageId"])
                candidates.append(candidate)
            tags = [{image["tag"] for image in c["images"].values()} for c in candidates]
            self.assertTrue(tags[0].isdisjoint(tags[1]))
            self.assertEqual(len(json.loads(state.read_text())), 16)


if __name__ == "__main__":
    unittest.main()
