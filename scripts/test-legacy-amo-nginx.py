#!/usr/bin/env python3
"""Exercise the shipped nginx retirement locations using an isolated container."""
import json
import pathlib
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


class LegacyAmoIngressTest(unittest.TestCase):
    def test_retired_routes_are_not_proxied_and_supported_routes_are(self):
        with tempfile.TemporaryDirectory(prefix="podk-02-nginx-") as directory:
            config = pathlib.Path(directory) / "nginx.conf"
            config.write_text("""events {}
http {
    access_log off;
    server { listen 8081; return 200 'upstream fixture'; }
    server {
        listen 8080;
        include /etc/nginx/legacy-amo-entry.locations.conf;
        location / { proxy_pass http://127.0.0.1:8081; }
    }
}
""")
            container = docker("create", "-p", "127.0.0.1::8080", "nginx:1.28-alpine")
            try:
                docker("cp", str(config), container + ":/etc/nginx/nginx.conf")
                docker("cp", str(ROOT / "deploy/nginx/legacy-amo-entry.locations.conf"), container + ":/etc/nginx/legacy-amo-entry.locations.conf")
                docker("start", container)
                port = docker("port", container, "8080/tcp").rsplit(":", 1)[1]
                base = f"http://127.0.0.1:{port}"
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                def request(path, method="POST"):
                    req = urllib.request.Request(base + path, data=b'{"selfUserId":"101","isAdmin":true,"isActive":true}', method=method)
                    req.add_header("Authorization", "Bearer old-browser-challenge")
                    try:
                        response = opener.open(req, timeout=5)
                    except urllib.error.HTTPError as error:
                        response = error
                    return response.status, response.read(), response.headers
                for _ in range(100):
                    try:
                        request("/healthz", "GET")
                        break
                    except (OSError, urllib.error.URLError):
                        time.sleep(0.1)
                else:
                    self.fail("nginx did not start")
                for route in ("admin-self-login", "company-bootstrap"):
                    for suffix in ("", "/", "?accountId=31355990", "/extra"):
                        for method in ("GET", "POST", "OPTIONS"):
                            with self.subTest(route=route, suffix=suffix, method=method):
                                status, body, headers = request(f"/api/v1/public/amocrm/{route}{suffix}", method)
                                self.assertEqual(status, 403)
                                self.assertEqual(json.loads(body)["error"]["code"], "AMO_BROWSER_ENTRY_DISABLED")
                                self.assertEqual(headers["Cache-Control"], "private, no-store")
                for route in ("/api/v1/auth/amocrm/entry-exchange", "/api/v1/provisioning/amocrm/widget-entries", "/api/v1/auth/login", "/api/v1/auth/access-link/fixture-employee-token"):
                    status, body, _ = request(route)
                    self.assertEqual((status, body), (200, b"upstream fixture"))
            finally:
                docker("rm", "-f", container)


if __name__ == "__main__":
    unittest.main()
