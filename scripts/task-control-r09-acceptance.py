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
parser.add_argument("phase", choices=["contracts", "unit", "postgres", "artifacts", "connection-backend"])
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
repositories = [("backend", backend)] if args.phase == "connection-backend" else [("backend", backend), ("core", args.core), ("widgets", args.widgets)]
for name, directory in repositories:
    manifest["repositories"][name] = {
        "path": str(directory.resolve()),
        "sha": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=directory, text=True).strip(),
        "status": subprocess.check_output(["git", "status", "--porcelain"], cwd=directory, text=True).splitlines(),
    }


def save():
    (out / "evidence.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")


def run(name, directory, command, extra=None, required=()):
    log = out / (name + ".log")
    print(name, flush=True)
    with log.open("w") as stream:
        result = subprocess.run(command, cwd=directory, env={**env, **(extra or {})}, stdout=stream, stderr=subprocess.STDOUT)
    check = {"name": name, "command": command, "cwd": str(directory), "exitCode": result.returncode, "log": str(log)}
    rejected = False
    if required:
        passed, skipped, failed = set(), set(), set()
        for line in log.read_text().splitlines():
            try:
                event = json.loads(line)
            except ValueError:
                continue
            if event.get("Test"):
                if event.get("Action") == "pass":
                    passed.add(event["Test"])
                elif event.get("Action") == "skip":
                    skipped.add(event["Test"])
                elif event.get("Action") == "fail":
                    failed.add(event["Test"])
        missing = sorted(set(required) - passed)
        check.update({"missingRequiredTests": missing, "skippedTests": sorted(skipped), "failedTests": sorted(failed)})
        rejected = bool(missing or skipped or failed)
    manifest["checks"].append(check)
    save()
    if result.returncode or rejected:
        print(f"FAILED: {log}", file=sys.stderr)
        sys.exit(result.returncode or 1)


if args.phase == "connection-backend":
    required = ("TestTaskControlHTTPStatusGuardsLinkTransaction", "TestTaskControlLifecycle",
                "TestTaskControlSaveAppliesRules", "TestTaskControlSaveAppliesRulesV2")
    selection = "^(" + "|".join(required) + ")$"
    run("connection-postgres", backend / "services/company",
        ["go", "test", "-race", "-tags=integration", "-json", "-count=1", "-timeout=10m", "-run", selection, "./internal/application"],
        {"GOWORK": "off"}, required)
elif args.phase == "contracts":
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
    runtime_version = json.loads((widget / "manifest.json").read_text())["widget"]["version"]
    modes = ["managed"] if tuple(map(int, runtime_version.split("."))) >= (1, 2, 5) else ["standalone", "managed"]
    for mode in modes:
        archive = out / f"task-control-{runtime_version}-{mode}-synthetic.zip"
        run(mode, widget, ["sh", "scripts/build.sh"], {
            "TASK_CONTROL_OUTPUT": str(archive), "TASK_CONTROL_STANDALONE_BUILD": "1" if mode == "standalone" else "0",
            "TASK_CONTROL_CORE_ORIGIN": "" if mode == "standalone" else "https://core.example.invalid",
            "TASK_CONTROL_BACKEND_ORIGIN": "" if mode == "standalone" else "https://backend.example.invalid",
            "COMPANY_ENTRY_CORE_ORIGIN": "https://core.example.invalid",
            "COMPANY_ENTRY_FRONTEND_ORIGIN": "https://teamos.example.invalid",
        })
        with zipfile.ZipFile(archive) as bundle:
            assert bundle.testzip() is None
            assert json.loads(bundle.read("manifest.json"))["widget"]["version"] == runtime_version
            deployment = bundle.read("lib/deployment.js").decode()
            assert ("https://core.example.invalid" in deployment) == (mode == "managed")
            if "lib/company-entry-deployment.js" in bundle.namelist():
                assert "https://core.example.invalid" in bundle.read("lib/company-entry-deployment.js").decode()
                assert "https://teamos.example.invalid" in bundle.read("lib/company-entry-deployment.js").decode()
            assert "script.js" in bundle.namelist()
            assert not any("tests/" in name or ".DS_Store" in name for name in bundle.namelist())
        manifest["artifacts"].append({"path": str(archive), "sha256": hashlib.sha256(archive.read_bytes()).hexdigest(), "mode": mode, "deployable": mode == "standalone"})
manifest["completedAt"] = datetime.now(timezone.utc).isoformat()
manifest["result"] = "passed"
save()
print(f"Local phase passed. Evidence: {out / 'evidence.json'}")
