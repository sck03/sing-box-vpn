#!/usr/bin/env python3
"""Public access/restart and bounded history acceptance on an isolated Linux VM.

Requires RUN_ACCESS_MAINTENANCE_TESTS=1, SB_TEST_ROOT and SB_TEST_PASSWORD.
Temporarily changes panel listeners and rotates only the project journal.
"""
import copy
import http.client
import json
import os
import pathlib
import socket
import ssl
import subprocess
import time

if os.environ.get("RUN_ACCESS_MAINTENANCE_TESTS") != "1":
    raise SystemExit("explicit disposable-VM opt-in is required")
root = pathlib.Path(os.environ["SB_TEST_ROOT"])
if not root.is_absolute() or not (root / ".platform-v4").is_file():
    raise SystemExit("expected an initialized disposable installation")
exec_start = subprocess.check_output(["systemctl", "show", "sb-panel", "-p", "ExecStart", "--value"], text=True)
if str(root) not in exec_start:
    raise SystemExit("sb-panel belongs to another installation")
port, secure, host, csrf, cookie = 2096, False, "127.0.0.1:2096", "", ""
certificate = root / "certificates" / "acceptance" / "cert.pem"
key = certificate.with_name("key.pem")
subprocess.run(["sudo", "install", "-d", "-m", "0700", str(certificate.parent)], check=True)
subprocess.run(["sudo", "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2",
                "-subj", "/CN=panel.example.test", "-addext", "subjectAltName=DNS:panel.example.test,DNS:sub.example.test,IP:127.0.0.1",
                "-keyout", str(key), "-out", str(certificate)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
subprocess.run(["sudo", "chmod", "0600", str(key)], check=True)
cert_pem = subprocess.check_output(["sudo", "cat", str(certificate)]).decode()
tls = ssl.create_default_context(cadata=cert_pem)


def request(path, data=None, *, endpoint=None, expected=200, retry_login=True):
    target_port, target_secure, target_host = endpoint or (port, secure, host)
    connection = (http.client.HTTPSConnection("127.0.0.1", target_port, context=tls, timeout=90)
                  if target_secure else http.client.HTTPConnection("127.0.0.1", target_port, timeout=90))
    try:
        connection.request("GET" if data is None else "POST", path,
                           None if data is None else json.dumps(data),
                           {"Host": target_host, "Cookie": cookie, "X-CSRF-Token": csrf, "Content-Type": "application/json"})
        response = connection.getresponse()
        raw = response.read()
        if response.status == 401 and path != "/api/auth/login" and retry_login:
            login()
            return request(path, data, endpoint=endpoint, expected=expected, retry_login=False)
        assert response.status == expected, (path, response.status, raw[:2048])
        try:
            value = json.loads(raw)
        except ValueError:
            value = raw.decode()
        return value, dict(response.getheaders())
    finally:
        connection.close()


def api(path, data=None, **kwargs):
    return request(path, data, **kwargs)[0]


def login():
    global csrf, cookie
    session, headers = request("/api/auth/login", {"username": "admin", "password": os.environ["SB_TEST_PASSWORD"]})
    csrf, cookie = session["csrf"], headers["Set-Cookie"].split(";", 1)[0]


def wait(identifier, state="succeeded", timeout=180):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            task = api("/api/tasks/detail?id=" + str(identifier))
            if task["state"] in ("succeeded", "failed", "cancelled"):
                assert task["state"] == state, task
                return task
        except (OSError, http.client.HTTPException):
            pass
        time.sleep(1)
    raise AssertionError("task timed out")


def apply(config):
    current = api("/api/config")
    wait(api("/api/config", {"config": config, "revision": current["revision"]})["task_id"])


def save_panel(options):
    current = api("/api/panel/settings")
    validated = api("/api/panel/settings/preview", {"options": options})["options"]
    api("/api/panel/settings", {"options": validated, "revision": current["revision"]})
    return api("/api/panel/settings")["revision"]


login()
original_config = api("/api/config")["config"]
original_panel = api("/api/panel/settings")["options"]
original_policy = api("/api/tasks/history")["policy"]
services = {s["name"]: s for s in api("/api/capabilities")["services"]}
assert services["sb-vpngate-core"]["section"] == "access"
assert services["sb-vpngate-gateway"]["section"] == "gateway"

try:
    invalid = copy.deepcopy(original_config)
    invalid["listener"] = {"enabled": True, "listen": "0.0.0.0", "port": 2107, "public_base_url": "https://sub.example.test:2107"}
    api("/api/config/preview", invalid, expected=400)
    independent = copy.deepcopy(invalid)
    independent["listener"].update({"certificate": str(certificate), "key": str(key)})
    apply(independent)
    endpoint = (2107, True, "sub.example.test:2107")
    assert "<html" in api("/user", endpoint=endpoint).lower()
    api("/user", endpoint=(2107, True, "wrong.example.test"), expected=400)
    apply(original_config)
    print("Public access: distinct service sections, URL-only rejection and independent TLS passed", flush=True)

    direct = {"listen": "127.0.0.1:2106", "public_url": "https://panel.example.test:2106",
              "certificate": str(certificate), "key": str(key), "trusted_proxy": []}
    revision = save_panel(direct)
    identifier = api("/api/panel/restart", {"revision": revision})["task_id"]
    port, secure, host = 2106, True, "panel.example.test:2106"
    assert wait(identifier)["result"]["phase"] == "succeeded"
    assert api("/api/panel/settings")["restart_required"] is False
    print("Panel restart: changed listener, direct TLS and durable completion passed", flush=True)

    broken = dict(direct, listen="127.0.0.1:2108", public_url="https://panel.example.test:2108")
    revision = save_panel(broken)
    with socket.socket() as occupied:
        occupied.bind(("127.0.0.1", 2108))
        occupied.listen()
        identifier = api("/api/panel/restart", {"revision": revision})["task_id"]
        task = wait(identifier, "failed")
    assert task["result"]["phase"] == "rolled-back", task
    assert api("/api/panel/settings")["options"] == direct
    api("/api/tasks/acknowledge", {"id": identifier})
    assert api("/api/tasks/detail?id=" + str(identifier))["acknowledged"] is True
    api("/api/tasks/delete", {"id": identifier})
    missing = api("/api/tasks/detail?id=" + str(identifier), expected=404)
    assert missing["error"]["code"] == "not_found", missing
    assert missing["error"]["retryable"] is False, missing
    policy = {"count": 20, "days": 1, "result_bytes": 262144}
    api("/api/tasks/history", policy)
    api("/api/tasks/history/prune", {})
    assert api("/api/tasks/history")["policy"] == policy
    print("Panel rollback: occupied listener restored; task handling/deletion and retention passed", flush=True)

    marker = "sb-vpn-global-" + str(time.time_ns())
    subprocess.run(["logger", "--tag", "sb-vpn-acceptance", marker], check=True)
    before = subprocess.check_output(["journalctl", "-t", "sb-vpn-acceptance", "--no-pager", "-n", "10"], text=True)
    assert marker in before
    assert api("/api/logs/usage")["namespace"] == "sb-vpn"
    api("/api/logs?service=sshd", expected=400)
    log, headers = request("/api/logs/download?service=sb-panel&lines=20")
    assert headers["Content-Type"].startswith("text/plain")
    api("/api/logs/clear", {})
    after = subprocess.check_output(["journalctl", "-t", "sb-vpn-acceptance", "--no-pager", "-n", "10"], text=True)
    assert marker in after
    print("Journal: download/filter/namespace-only cleanup preserved global logs", flush=True)
finally:
    # Return the disposable installation to its original access and limits.
    try:
        apply(original_config)
        api("/api/tasks/history", original_policy)
        revision = save_panel(original_panel)
        identifier = api("/api/panel/restart", {"revision": revision})["task_id"]
        port, secure, host = 2096, False, "127.0.0.1:2096"
        wait(identifier)
    except Exception as error:
        print("Restoration failed:", type(error).__name__, flush=True)
        raise
print("Access and maintenance Linux acceptance passed.")
