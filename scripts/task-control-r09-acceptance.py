#!/usr/bin/env python3
"""Reproduce synthetic R09 evidence; never connects to a live CRM or deploys.

Each phase records exact checkout revisions, commands, exit codes and log paths.
An exit code of zero certifies only the selected local phase, never live acceptance.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from datetime import datetime, timezone

backend = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("phase", choices=["contracts", "unit", "postgres", "artifacts"])
parser.add_argument("--core", type=Path, default=backend.parent / "amocrm-pro")
parser.add_argument("--widgets", type=Path, default=backend.parent / "rakurs/rakurs-widgets-new")
parser.add_argument("--output", type=Path)
args = parser.parse_args()
out = (args.output or Path(tempfile.mkdtemp(prefix="task-control-r09-"))).resolve()
out.mkdir(parents=True, exist_ok=True)
widget = args.widgets.resolve() / "rkrs_lead_notasks"
env = dict(os.environ, WIDGET_DIR=str(widget))
manifest = {"kind": "synthetic-local", "phase": args.phase,
            "startedAt": datetime.now(timezone.utc).isoformat(), "repositories": {}, "checks": [], "artifacts": []}
for name, directory in [("backend", backend), ("core", args.core), ("widgets", args.widgets)]:
    manifest["repositories"][name] = {
        "path": str(directory.resolve()),
        "sha": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=directory, text=True).strip(),
        "status": subprocess.check_output(["git", "status", "--porcelain"], cwd=directory, text=True).splitlines(),
    }


def save():
    (out / "evidence.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")


def run(name, directory, command, extra=None):
    log = out / (name + ".log")
    print(name, flush=True)
    with log.open("w") as stream:
        result = subprocess.run(command, cwd=directory, env={**env, **(extra or {})}, stdout=stream, stderr=subprocess.STDOUT)
    manifest["checks"].append({"name": name, "command": command, "cwd": str(directory), "exitCode": result.returncode, "log": str(log)})
    save()
    if result.returncode:
        print(f"FAILED: {log}", file=sys.stderr)
        sys.exit(result.returncode)


if args.phase == "contracts":
    run("openapi-proto-frontend", backend, ["make", "check-contract"])
    for version in ["widget", "v2-widget", "import-widget"]:
        run(version, backend, ["node", f"scripts/check-task-control-{version}-contract.cjs"])
elif args.phase == "unit":
    for service in ["company", "gateway"]:
        run(service, backend / "services" / service, ["go", "test", "-race", "-count=1", "./..."], {"GOWORK": "off"})
    run("core", args.core, ["go", "test", "-race", "-count=1", "./cmd/...", "./internal/...", "./api/..."])
    run("widget", widget, ["npm", "test"])
    run("widget-syntax", widget, ["npm", "run", "check"])
    distribution = args.widgets / "rkrs_lead_distribution/widget-app"
    run("distribution-build", distribution, ["npm", "run", "build:preview"])
    run("distribution", distribution, ["npm", "test"])
elif args.phase == "postgres":
    run("company-postgres-widget-roundtrip", backend / "services/company", ["go", "test", "-race", "-tags", "integration", "-count=1", "-v", "-timeout=12m", "-run", "TaskControl", "./internal/application"], {"GOWORK": "off"})
    run("core-postgres-migrations", args.core, ["make", "integration-test"])
elif args.phase == "artifacts":
    import zipfile
    for mode in ["standalone", "managed"]:
        archive = out / f"task-control-1.2.3-{mode}-synthetic.zip"
        run(mode, widget, ["sh", "scripts/build.sh"], {
            "TASK_CONTROL_OUTPUT": str(archive), "TASK_CONTROL_STANDALONE_BUILD": "1" if mode == "standalone" else "0",
            "TASK_CONTROL_CORE_ORIGIN": "" if mode == "standalone" else "https://core.example.invalid",
            "TASK_CONTROL_BACKEND_ORIGIN": "" if mode == "standalone" else "https://backend.example.invalid",
        })
        with zipfile.ZipFile(archive) as bundle:
            assert bundle.testzip() is None
            assert json.loads(bundle.read("manifest.json"))["widget"]["version"] == "1.2.3"
            deployment = bundle.read("lib/deployment.js").decode()
            assert ("https://core.example.invalid" in deployment) == (mode == "managed")
            assert "script.js" in bundle.namelist()
            assert not any("tests/" in name or ".DS_Store" in name for name in bundle.namelist())
        manifest["artifacts"].append({"path": str(archive), "sha256": hashlib.sha256(archive.read_bytes()).hexdigest(), "mode": mode, "deployable": mode == "standalone"})
manifest["completedAt"] = datetime.now(timezone.utc).isoformat()
manifest["result"] = "passed"
save()
print(f"Local phase passed. Evidence: {out / 'evidence.json'}")
