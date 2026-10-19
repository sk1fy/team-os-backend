#!/usr/bin/env python3
"""Read-only artifact comparison. Does not certify installation, live acceptance or rollout."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import re
import subprocess
import uuid
import zipfile
from urllib.parse import urlsplit

COMPONENTS = {"core/api", "core/worker", "backend/company", "backend/gateway"}


def inspect_container(container):
    # Select only non-secret fields; never record Docker Config.Env or raw inspect.
    raw = subprocess.check_output([
        "docker", "container", "inspect", "--format",
        '{{json .Image}}\n{{json .State.Running}}', container,
    ], text=True, stderr=subprocess.DEVNULL).splitlines()
    image_id, running = map(json.loads, raw)
    raw = subprocess.check_output([
        "docker", "image", "inspect", "--format",
        '{{json .Config.Labels}}\n{{json .Architecture}}', image_id,
    ], text=True, stderr=subprocess.DEVNULL).splitlines()
    labels, architecture = map(json.loads, raw)
    return {"localImageId": image_id, "running": running,
            "revision": (labels or {}).get("org.opencontainers.image.revision"),
            "architecture": architecture}


def compare(candidate, observed, archive, mode):
    errors = []
    if not candidate.get("localBuildPassed") or not candidate.get("imagesComplete"):
        errors.append("Кандидат не содержит завершённую сборку восьми образов")
    for component in sorted(COMPONENTS):
        expected = candidate.get("images", {}).get(component, {})
        actual = observed.get(component, {})
        repo = component.split("/")[0]
        sha = candidate.get("repositories", {}).get(repo, {}).get("sha", "")
        if not re.fullmatch(r"[a-f0-9]{40}", sha) or expected.get("revision") != sha:
            errors.append(f"{component}: нет согласованного SHA в candidate.json")
        if not actual.get("running"):
            errors.append(f"{component}: контейнер не запущен или недоступен")
        for field in ["localImageId", "revision", "architecture"]:
            if not expected.get(field) or actual.get(field) != expected[field]:
                errors.append(f"{component}: {field} не совпадает с кандидатом")
    checksum = hashlib.sha256(archive.read_bytes()).hexdigest()
    if checksum != candidate.get("archives", {}).get(mode, {}).get("sha256"):
        errors.append("SHA-256 ZIP не совпадает с кандидатом")
    with zipfile.ZipFile(archive) as package:
        if package.testzip() is not None:
            errors.append("Нарушена целостность ZIP")
        version = json.loads(package.read("manifest.json"))["widget"]["version"]
    if version != candidate.get("runtimeVersion"):
        errors.append("Версия manifest.json не совпадает с кандидатом")
    if mode == "managed":
        config = candidate.get("publicConfiguration", {})
        for field in ["coreOrigin", "backendOrigin"]:
            parsed = urlsplit(config.get(field, ""))
            hostname = parsed.hostname or ""
            if (parsed.scheme != "https" or not hostname or hostname.endswith(".invalid")
                    or parsed.username or parsed.password or parsed.path not in ["", "/"]
                    or parsed.query or parsed.fragment):
                errors.append(f"{field}: нет пригодного HTTPS origin")
        try:
            if uuid.UUID(config.get("oauthClientUUID") or "").int == 0:
                raise ValueError()
        except ValueError:
            errors.append("Не указан действительный OAuth UUID Контроля задач")
    return {"artifactMatch": not errors, "errors": errors, "observedImages": observed,
            "archive": {"sha256": checksum, "version": version, "mode": mode},
            "acceptancePassed": False, "releaseApproved": False,
            "remainingGates": [
                "Schema Core/company 30/40: проверить на целевых БД",
                "Установленный в amoCRM ZIP: сверить отдельно; файл на диске не доказывает установку",
                "Installation/generation, runtime descriptor, webhook, binding и freshness",
                "R09 live/browser/ACK и regression matrix",
                "R10 full-stack rollout, backup restore и binary rollback",
                "CI финальных SHA и registry digests (локальный image ID не доказывает публикацию)",
            ]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--container", action="append", required=True, metavar="COMPONENT=NAME")
    parser.add_argument("--widget-zip", type=Path, required=True)
    parser.add_argument("--mode", choices=["managed", "standalone"], default="managed")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    containers = {}
    for mapping in args.container:
        component, separator, name = mapping.partition("=")
        if not separator or not name or component not in COMPONENTS or component in containers:
            parser.error("Задайте каждый из core/api, core/worker, backend/company, backend/gateway один раз")
        containers[component] = name
    if containers.keys() != COMPONENTS:
        parser.error("Нужны четыре контейнера: core/api, core/worker, backend/company, backend/gateway")
    observed = {}
    for component, name in containers.items():
        try:
            observed[component] = inspect_container(name)
        except (subprocess.CalledProcessError, OSError, ValueError):
            observed[component] = {}  # No raw Docker errors or environment in the report.
    try:
        report = compare(json.loads(args.candidate.read_text()), observed, args.widget_zip, args.mode)
    except (OSError, ValueError, KeyError, TypeError, zipfile.BadZipFile):
        report = {"artifactMatch": False, "acceptancePassed": False, "releaseApproved": False,
                  "errors": ["Не удалось прочитать candidate.json или корректный ZIP"]}
    report["checkedAt"] = datetime.now(timezone.utc).isoformat()
    with args.output.open("x", encoding="utf-8") as output:
        json.dump(report, output, ensure_ascii=False, indent=2)
        output.write("\n")
    print(f"artifactMatch={report['artifactMatch']}; acceptancePassed=false; отчёт: {args.output}")
    return 0 if report["artifactMatch"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
