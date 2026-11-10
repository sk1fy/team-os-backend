#!/usr/bin/env python3
"""Run registration, access and employee-import acceptance using isolated fixtures."""
import argparse
from collections import deque
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
SOURCE_TESTS = (
    "TestClientFetchAll",
    "TestClientFetchAllRejectsBusinessErrorAndMissingData",
    "TestClientFetchAllResponseBoundary",
    "TestClientFetchAllCancellationAndReadFailure",
)
INTEGRATION_TESTS = (
    "TestAccountAccessRegistrationRejectionsAreAtomic",
    "TestAccountAccessRegistrationTokenHasOneWinner",
    "TestAccountAccessWidgetAdminAndEmployeeLifecycle",
    "TestCompanyRegistrationTokenLifecycle",
    "TestAmoCompanyEntryTransactions",
    "TestSessionConcurrentRefreshAndLogoutScope",
    "TestRefreshFamilyRecovery",
    "TestEmployeeAccessManagementRolesAndLifecycle",
    "TestEmployeeSectionsAndLifecycle",
    "TestLoginPasswordQueueDoesNotHoldDatabaseAndRechecksAccess",
    "TestBrowserEntryCannotCreateCompanyOrPromoteEmployee",
    "TestAmoImportHTTPToPostgres",
    "TestAmoImportHTTPFailuresPreservePostgres",
    "TestAmoImportCreatesDepartmentsAndAssignsEmployees",
    "TestAmoImportAllowsSameEmployeeInDifferentCompanies",
    "TestTaskControlSharedMappingPreservesAccountProvenance",
)


def run(name, module, args, required, log_dir):
    command = ["go", "test", "-race", "-json", "-count=1", "-timeout=15m", *args]
    print(f"{name}: running", flush=True)
    env = {**os.environ, "GOWORK": "off"}
    process = subprocess.Popen(command, cwd=ROOT / module, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    passed, skipped, failed = set(), set(), set()
    lines = deque(maxlen=40)
    log = (log_dir / f"{name}.jsonl").open("w") if log_dir else None
    for line in process.stdout:
        lines.append(line)
        if log:
            log.write(line)
            log.flush()
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        test = event.get("Test")
        if not test:
            continue
        action = event.get("Action")
        if action == "pass":
            passed.add(test)
        elif action == "skip":
            skipped.add(test)
        elif action == "fail":
            failed.add(test)
    status = process.wait()
    if log:
        log.close()
    missing = set(required) - passed
    print(f"{name}: exit={status}; passed={len(passed)}; skipped={len(skipped)}; failed={len(failed)}", flush=True)
    if status or missing or failed or skipped:
        if missing:
            print("Required tests did not pass: " + ", ".join(sorted(missing)), file=sys.stderr)
        print("".join(lines), file=sys.stderr)
        return False
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--unit-only", action="store_true")
    mode.add_argument("--integration-only", action="store_true")
    parser.add_argument("--log-dir", type=Path)
    args = parser.parse_args()
    if args.log_dir:
        args.log_dir.mkdir(parents=True, exist_ok=True)
    jobs = []
    source_selection = "^(" + "|".join(SOURCE_TESTS) + ")$"
    jobs.append(("employee-source", "services/company", ["./internal/externalusers", "-run", source_selection], SOURCE_TESTS))
    if not args.integration_only:
        jobs.extend([
            ("gateway", "services/gateway", ["./internal/transport", "./internal/authmw", "./internal/config"], ["TestGatewayLoginBridgesJSONToGRPCAndSetsRefreshCookie", "TestGatewayRefreshReadsAndRotatesCookie", "TestBrowserEntryDeniedBeforeReadingIdentityOrCallingCompany", "TestProvisioningCannotTrustBrowserWithoutServiceCredential"]),
            ("company", "services/company", ["./internal/application", "./internal/transport/grpc"], ["TestLoginWithRevokedAccessLinkIsRejected", "TestBrowserEntryCannotUseDatabaseOrImporter", "TestBrowserEntryDeniedEvenWithGatewayCredential"]),
        ])
    if not args.unit_only:
        selection = "^(" + "|".join(INTEGRATION_TESTS) + ")$"
        jobs.append(("postgres", "services/company", ["-tags=integration", "./internal/application", "-run", selection], INTEGRATION_TESTS))
    for name, module, options, required in jobs:
        if not run(name, module, options, required, args.log_dir):
            return 1
    print("PASS: unit matrix only; PostgreSQL acceptance NOT RUN" if args.unit_only else "PASS: selected account-access acceptance matrix")
    return 0


if __name__ == "__main__":
    sys.exit(main())
