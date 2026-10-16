#!/usr/bin/env python3
"""Build a local Task Control candidate. Does not push, deploy or approve rollout."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid
from urllib.parse import urlsplit
import zipfile
import zlib

backend = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--core", type=Path, default=backend.parent / "amocrm-pro")
parser.add_argument("--widgets", type=Path, default=backend.parent / "rakurs/rakurs-widgets-new")
parser.add_argument("--output", type=Path, required=True)
parser.add_argument("--core-origin", default="https://core.example.invalid")
parser.add_argument("--backend-origin", default="https://backend.example.invalid")
parser.add_argument("--oauth-client-uuid")
parser.add_argument("--build-images", action="store_true")
args = parser.parse_args()
for origin in [args.core_origin, args.backend_origin]:
    parsed_origin = urlsplit(origin)
    if parsed_origin.scheme != "https" or not parsed_origin.hostname or parsed_origin.username or parsed_origin.password or parsed_origin.path not in ["", "/"] or parsed_origin.query or parsed_origin.fragment:
        parser.error("Expected public HTTPS origins without paths, credentials, queries or fragments")
if args.oauth_client_uuid:
    parsed = uuid.UUID(args.oauth_client_uuid)
    if parsed.int == 0:
        parser.error("OAuth client UUID cannot be nil")
out = args.output.resolve()
out.mkdir(parents=True, exist_ok=False)
os.chmod(out, 0o700)
manifest = {"kind": "local-release-candidate", "runtimeVersion": "1.2.3", "releaseApproved": False,
            "createdAt": datetime.now(timezone.utc).isoformat(), "repositories": {}, "images": {}, "archives": {},
            "publicConfiguration": {"coreOrigin": args.core_origin, "backendOrigin": args.backend_origin, "oauthClientUUID": args.oauth_client_uuid},
            "schema": {"core": 30, "company": 40, "runtime": [1, 2]},
            "toolchain": {"python": sys.version.split()[0], "zlib": zlib.ZLIB_VERSION},
            "remainingGates": ["R09 live acceptance deferred", "browser acceptance deferred", "CI on final R10 commits", "registry image digests after publishing", "full-stack rollout and binary rollback on approved environment", "verify deployment architecture", "operator verifies current installation/generation and OAuth UUID"]}


def save():
    (out / "candidate.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")


def git(directory, *arguments):
    return subprocess.check_output(["git", *arguments], cwd=directory, text=True).strip()


def run(name, directory, command, extra=None):
    with (out / (name + ".log")).open("w") as log:
        result = subprocess.run(command, cwd=directory, env={**os.environ, **(extra or {})}, stdout=log, stderr=subprocess.STDOUT)
    if result.returncode:
        manifest["failedStep"] = name
        save()
        raise SystemExit(f"{name} failed; see {out / (name + '.log')}")


def verify_sources():
    for name, item in manifest["repositories"].items():
        if git(item["path"], "rev-parse", "HEAD") != item["sha"] or git(item["path"], "status", "--porcelain", "--untracked-files=no"):
            manifest["failedStep"] = "source-changed-" + name
            save()
            raise SystemExit("Checkout changed during candidate build; do not use partial artifacts")


for name, directory in [("core", args.core), ("backend", backend), ("widgets", args.widgets)]:
    if git(directory, "status", "--porcelain", "--untracked-files=no"):
        raise SystemExit(f"Commit tracked changes first: {directory}")
    # Untracked test logs/designs outside the widget package are not packaged.
    untracked = git(directory, "ls-files", "--others", "--exclude-standard").splitlines()
    relevant = untracked if name != "widgets" else [f for f in untracked if f.startswith("rkrs_lead_notasks/") and not f.endswith("/.DS_Store")]
    if relevant:
        raise SystemExit(f"Untracked source files in {directory}; commit or move them before packaging")
    manifest["repositories"][name] = {"sha": git(directory, "rev-parse", "HEAD"), "path": str(directory.resolve())}
widget = args.widgets / "rkrs_lead_notasks"
epoch = git(args.widgets, "show", "-s", "--format=%ct", "HEAD")
for mode in ["standalone", "managed"]:
    paths = [out / f"task-control-1.2.3-{mode}{suffix}.zip" for suffix in ["", "-repeat"]]
    for index, archive in enumerate(paths):
        run(f"zip-{mode}-{index}", widget, ["sh", "scripts/build.sh"], {
            "SOURCE_DATE_EPOCH": epoch, "TASK_CONTROL_OUTPUT": str(archive),
            "TASK_CONTROL_STANDALONE_BUILD": "1" if mode == "standalone" else "0",
            "TASK_CONTROL_CORE_ORIGIN": args.core_origin if mode == "managed" else "",
            "TASK_CONTROL_BACKEND_ORIGIN": args.backend_origin if mode == "managed" else "",
        })
    if paths[0].read_bytes() != paths[1].read_bytes():
        raise SystemExit("ZIP rebuild differs; do not release")
    paths[1].unlink()
    with zipfile.ZipFile(paths[0]) as archive:
        if archive.testzip() is not None or json.loads(archive.read("manifest.json"))["widget"]["version"] != "1.2.3":
            raise SystemExit("invalid ZIP/version")
    manifest["archives"][mode] = {"path": str(paths[0]), "sha256": hashlib.sha256(paths[0].read_bytes()).hexdigest(), "sourceDateEpoch": int(epoch), "rebuildIdentical": True}
    save()
verify_sources()
if args.build_images:
    targets = [("core", target, ["--target", target]) for target in ["api", "worker", "migrate", "integrations", "task-control-grant", "task-control-runtime-descriptor"]]
    targets += [("backend", service, ["-f", f"services/{service}/Dockerfile"]) for service in ["company", "gateway"]]
    for repo, component, options in targets:
        verify_sources()
        directory = args.core if repo == "core" else backend
        sha = manifest["repositories"][repo]["sha"]
        image = f"task-control-rc-{repo}-{component}:{sha[:12]}"
        print(f"Building {image}", flush=True)
        run(f"image-{repo}-{component}", directory, ["docker", "build", *options,
            "--build-arg", f"BUILD_REVISION={sha}", "--label", f"org.opencontainers.image.revision={sha}", "-t", image, "."])
        verify_sources()
        raw = subprocess.check_output(["docker", "image", "inspect", "--format", '{{json .Id}}\n{{json .RepoDigests}}\n{{json .Config.Labels}}\n{{json .Architecture}}', image], text=True).splitlines()
        image_id, digests, labels, architecture = map(json.loads, raw)
        if labels.get("org.opencontainers.image.revision") != sha:
            raise SystemExit("image revision mismatch")
        manifest["images"][repo + "/" + component] = {"tag": image, "localImageId": image_id, "repositoryDigests": digests or [], "architecture": architecture, "revision": sha}
        save()
    for repo in ["core", "backend"]:
        services = {}
        for key, item in manifest["images"].items():
            owner, service = key.split("/", 1)
            if owner == repo:
                services[service] = {"image": (item["repositoryDigests"] or [item["localImageId"]])[0]}
        filename = out / (repo + ".compose.json")
        filename.write_text(json.dumps({"services": services}, indent=2) + "\n")
    manifest["composeOverrides"] = {"core": str(out / "core.compose.json"), "backend": str(out / "backend.compose.json")}
verify_sources()
manifest["completedAt"] = datetime.now(timezone.utc).isoformat()
manifest["imagesComplete"] = len(manifest["images"]) == 8
if not manifest["imagesComplete"]:
    manifest["remainingGates"].append("eight application/tool images not built")
manifest["localBuildPassed"] = True
save()
print(f"Local candidate: {out / 'candidate.json'}; rollout is not approved")
