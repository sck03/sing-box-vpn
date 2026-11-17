#!/usr/bin/env python3
"""Destructive acceptance on a disposable v4 installation only.

Run on the isolated Linux VM with SB_TEST_PASSWORD in the environment.
Creates one disabled test entitlement; changes ordinary log verbosity and
temporarily stops/restarts the ordinary instance, then verifies rollback.
"""
import copy
import hashlib
import http.cookiejar
import json
import os
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.request
import urllib.error

base = os.environ.get("SB_TEST_URL", "http://127.0.0.1:2096")
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ""

def api(path, data=None):
    body = None if data is None else json.dumps(data).encode()
    request = urllib.request.Request(base + path, data=body, headers={"Content-Type": "application/json", "X-CSRF-Token": csrf})
    with opener.open(request, timeout=180) as response:
        return json.load(response)

def wait(task, expected="succeeded"):
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        try:
            value = api("/api/tasks/detail?id=" + str(task))
        except urllib.error.HTTPError as error:
            if error.code != 503:
                raise
            time.sleep(1)
            continue
        if value["state"] in ["failed", "succeeded", "cancelled"]:
            assert value["state"] == expected, value
            return value
        time.sleep(1)
    raise AssertionError("task timed out")

def task(**action):
    return wait(api("/api/tasks", action)["task_id"])

def apply(config, expected="succeeded"):
    for _ in range(30):
        state = api("/api/config")
        try:
            identifier = api("/api/config", {"revision": state["revision"], "config": config})["task_id"]
            return wait(identifier, expected)
        except urllib.error.HTTPError as e:
            detail=e.read().decode()
            if "wait for the current task" not in detail:
                raise AssertionError(detail) from e
            time.sleep(1)
    raise AssertionError("queue never became idle")

ready_deadline = time.monotonic() + 60
while True:
    try:
        session = api("/api/auth/login", {"username": "admin", "password": os.environ["SB_TEST_PASSWORD"]})
        break
    except urllib.error.HTTPError:
        raise
    except urllib.error.URLError:
        if time.monotonic() >= ready_deadline:
            raise
        time.sleep(1)
csrf = session["csrf"]
original = api("/api/config")["config"]
identifier = "linux-" + str(time.time_ns())
account = {"id": identifier, "name": "Linux acceptance", "quota_bytes": 1048576, "revision": 0, "start_mode": "first_use", "period": {"kind": "monthly", "day": 31, "timezone": "Asia/Shanghai"}}
api("/api/accounts", {"account": account})
task(operation="account", account={"operation": "bind", "id": account["id"], "scope": "normal"})
export = api("/api/accounts/export", {"id": account["id"], "client": "uri", "scope": "normal"})
assert len(export["content"].strip().splitlines()) == 5
assert api("/api/qr", {"content": export["content"].splitlines()[0]})["image"].startswith("data:image/png;base64,")
grant = api("/api/grants", {"id": identifier, "account_id": account["id"], "scope": "normal", "clients": ["uri"], "max_downloads": 1})
assert "/user#" in grant["user_url"]
download_url = base + "/u/" + grant["token"] + "/uri"
with opener.open(download_url, timeout=30) as response:
    assert len(response.read().decode().strip().splitlines()) == 5
try:
    opener.open(download_url, timeout=30)
    raise AssertionError("download limit was bypassed")
except urllib.error.HTTPError as error:
    assert error.code in [403, 404], error.code
usage=next(a for a in api("/api/accounts") if a["id"]==account["id"])
assert usage["started_at"] is None
print("HTTP: login, independent account, binding, export, QR and first-use grant passed",flush=True)

task(operation="service", service="sing-box", value="stop")
changed=copy.deepcopy(original)
changed["app"].setdefault("advanced",{})["log"]={"level":"warn"}
apply(changed)
assert subprocess.run(["systemctl","is-active","--quiet","sing-box"]).returncode!=0
task(operation="service", service="sing-box", value="start")
apply(original)
print("systemd: stopped state preserved after configuration application",flush=True)

enabled=subprocess.check_output(["systemctl","is-enabled","sing-box"],text=True).strip()
with socket.socket() as occupied:
    occupied.bind(("0.0.0.0",0));occupied.listen()
    bad=copy.deepcopy(original)
    node=next(v for v in bad["app"]["nodes"] if v["protocol"]=="vless")
    node["port"]=occupied.getsockname()[1]
    result=apply(bad,"failed")
    assert result["phase"]=="rolled-back",result
assert subprocess.check_output(["systemctl","is-enabled","sing-box"],text=True).strip()==enabled
assert subprocess.run(["systemctl","is-active","--quiet","sing-box"]).returncode==0
apply(original)
usage=next(a for a in api("/api/accounts") if a["id"]==account["id"])
account["revision"]=usage["revision"];account["disabled"]=True
api("/api/accounts",{"account":account})
api("/api/grants/revoke",{"id":identifier})
print("systemd: failed runtime application rolled back configuration and active/enabled state",flush=True)

# Verify a certificate update reaches a real TLS listener, then restore it.
with tempfile.TemporaryDirectory(prefix="sbvpn-cert-accept-") as directory:
    cert_path, key_path = directory + "/cert.pem", directory + "/key.pem"
    subprocess.run(["openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:prime256v1",
                    "-nodes", "-keyout", key_path, "-out", cert_path, "-days", "2", "-subj", "/CN=localhost",
                    "-addext", "subjectAltName=DNS:localhost"], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    with open(cert_path) as cert_file, open(key_path) as key_file:
        cert, key = cert_file.read(), key_file.read()
    asset = api("/api/certificates", {"id": identifier + "-cert", "domain": "localhost", "certificate": cert, "key": key})
    updated = copy.deepcopy(original)
    updated["certificates"].append(asset)
    node = next(v for v in updated["app"]["nodes"] if v["protocol"] == "anytls")
    node["tls"].update({"certificate": asset["certificate"], "key": asset["key"], "server_name": "localhost"})
    try:
        apply(updated)
        with socket.create_connection(("127.0.0.1", node["port"]), timeout=10) as connection:
            with ssl._create_unverified_context().wrap_socket(connection, server_hostname="localhost") as secured:
                actual = hashlib.sha256(secured.getpeercert(binary_form=True)).digest()
        assert actual == hashlib.sha256(ssl.PEM_cert_to_DER_cert(cert)).digest()
    finally:
        apply(original)
print("TLS: imported certificate reached the live listener; original configuration restored", flush=True)
api("/api/auth/logout",{})
