"""Exercise the connection CLI with disposable Git metadata and fake Go JSON events."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class ConnectionAcceptanceTest(unittest.TestCase):
    def test_required_tests_cannot_silently_skip_or_disappear(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            backend = root / "backend"
            (backend / "scripts").mkdir(parents=True)
            (backend / "services/company").mkdir(parents=True)
            script = backend / "scripts/task-control-r09-acceptance.py"
            shutil.copyfile(Path(__file__).with_name(script.name), script)
            for command in [["init", "-q"], ["add", "."], ["-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture"]]:
                subprocess.run(["git", *command], cwd=backend, check=True, capture_output=True)
            executables = root / "bin"
            executables.mkdir()
            go = executables / "go"
            go.write_text('''#!/usr/bin/env python3
import json, os, sys
selection = sys.argv[sys.argv.index('-run')+1]
names = selection[2:-2].split('|')
for index, name in enumerate(names):
    mode = os.environ['FAKE_GO_RESULT']
    if index == 0 and mode == 'missing':
        continue
    action = 'skip' if index == 0 and mode == 'skip' else 'pass'
    print(json.dumps({'Test':name, 'Action':action}))
''')
            go.chmod(0o700)
            for result, exit_code in [("pass", 0), ("skip", 1), ("missing", 1)]:
                with self.subTest(result=result):
                    output = root / result
                    process = subprocess.run(["python3", str(script), "connection-backend", "--output", str(output)],
                                             env={**os.environ, "PATH": str(executables) + os.pathsep + os.environ["PATH"], "FAKE_GO_RESULT": result},
                                             text=True, capture_output=True)
                    self.assertEqual(process.returncode, exit_code, process.stdout + process.stderr)
                    evidence = json.loads((output / "evidence.json").read_text())
                    self.assertEqual(set(evidence["repositories"]), {"backend"})
                    if result == "pass":
                        self.assertEqual(evidence["result"], "passed")
                    else:
                        self.assertNotEqual(evidence.get("result"), "passed")
                        check = evidence["checks"][0]
                        self.assertTrue(check["skippedTests"] or check["missingRequiredTests"])


if __name__ == "__main__":
    unittest.main()
