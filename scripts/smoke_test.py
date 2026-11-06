#!/usr/bin/env python3
"""Run a real Terraform CRUD lifecycle against a local mock Onidel API.

Build the provider first, then run:
  python3 scripts/smoke_test.py --terraform /path/to/terraform
No Onidel token, account, or paid API calls are used.
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
VM_ID = "c1d45377-b2a9-4407-ab8d-6909c34dfaac"
TYPE_ID = "ea645d53-c0e5-406d-8863-9ecd4235be94"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--terraform", default="terraform")
    args = parser.parse_args()
    remote = {"vm": None, "calls": []}

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def reply(self, code, value=None):
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            if value is not None:
                self.wfile.write(json.dumps(value).encode())

        def dispatch(self):
            assert self.headers.get("Authorization") == "Bearer smoke-test-token"
            path = urlsplit(self.path).path
            remote["calls"].append((self.command, path))
            if self.command == "POST" and path == "/vm":
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                assert body["os"] == 2 and body["cpu"] == 2
                assert remote["vm"] is None, "provider unexpectedly created twice"
                remote["vm"] = {
                    "id": VM_ID, "name": body["name"], "password": "mock-secret",
                    "vcpu": body["cpu"], "ram": body["ram"], "disk": body["disk"],
                    "main_ipv4": "192.0.2.10", "main_ipv6": "", "status": "active",
                    "template": "Ubuntu", "boot_mode": "bios", "firewall_group_id": None,
                }
                self.reply(201, {"id": VM_ID})
            elif path == f"/vm/{VM_ID}" and self.command == "GET":
                self.reply(200 if remote["vm"] else 404, remote["vm"])
            elif path == f"/vm/{VM_ID}" and self.command == "PATCH":
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                assert set(body) == {"name"}, body
                remote["vm"]["name"] = body["name"]
                self.reply(202)
            elif path == f"/vm/{VM_ID}" and self.command == "DELETE":
                remote["vm"] = None
                self.reply(204)
            else:
                self.reply(404)

        do_GET = dispatch
        do_POST = dispatch
        do_PATCH = dispatch
        do_DELETE = dispatch

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        with tempfile.TemporaryDirectory(prefix="onidel-smoke-") as directory:
            work = Path(directory)
            cli_config = work / "terraform.rc"
            cli_config.write_text(
                'provider_installation {\n  dev_overrides {\n'
                f'    "registry.terraform.io/solaris-soft/onidel" = {json.dumps(str(ROOT))}\n'
                '  }\n  direct {}\n}\n'
            )
            env = dict(os.environ, TF_CLI_CONFIG_FILE=str(cli_config), TF_IN_AUTOMATION="1",
                       CHECKPOINT_DISABLE="1", ONIDEL_API_TOKEN="smoke-test-token",
                       ONIDEL_ENDPOINT=f"http://127.0.0.1:{server.server_port}")
            env.pop("ONIDEL_TEAM_ID", None)
            config = '''terraform {
  required_providers {
    onidel = { source = "solaris-soft/onidel" }
  }
}
provider "onidel" {}
resource "onidel_virtual_machine" "test" {
  name = "NAME"
  payment_cycle = "hourly"
  instance_type = "TYPE"
  location = "Sydney"
  cpu = 2
  ram = 4096
  disk = 40
  os = 2
}
'''.replace("TYPE", TYPE_ID)

            def configure(name):
                (work / "main.tf").write_text(config.replace("NAME", name))

            def tf(*commands, expected=0):
                result = subprocess.run([args.terraform, *commands, "-no-color"], cwd=work,
                                        env=env, text=True, stdout=subprocess.PIPE,
                                        stderr=subprocess.STDOUT)
                if result.returncode != expected:
                    raise RuntimeError(f"terraform {' '.join(commands)} failed:\n{result.stdout}")
                return result.stdout

            configure("smoke-original")
            tf("validate")
            tf("apply", "-auto-approve", "-input=false")
            tf("plan", "-detailed-exitcode", "-input=false")
            configure("smoke-renamed")
            tf("apply", "-auto-approve", "-input=false")
            assert remote["vm"]["name"] == "smoke-renamed"
            tf("plan", "-detailed-exitcode", "-input=false")
            (work / "lookup.tf").write_text(
                f'data "onidel_virtual_machine" "test" {{ id = "{VM_ID}" }}\n'
            )
            tf("apply", "-auto-approve", "-input=false")
            (work / "lookup.tf").unlink()
            tf("destroy", "-auto-approve", "-input=false")
            assert remote["vm"] is None
            assert sum(method == "POST" for method, _ in remote["calls"]) == 1
            assert sum(method == "PATCH" for method, _ in remote["calls"]) == 1
            assert sum(method == "DELETE" for method, _ in remote["calls"]) == 1
            print("Terraform smoke test passed: create, no-op plan, rename, lookup, destroy.")
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
