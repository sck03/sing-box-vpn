#!/usr/bin/env python3
"""Read-only sustained panel workload against a disposable Linux installation.

SB_TEST_PASSWORD is required. SB_TEST_URL defaults to loopback:2096.
SB_LOAD_SECONDS defaults to 60; no credentials or response bodies are reported.
"""
import concurrent.futures
import http.cookiejar
import json
import os
import pathlib
import statistics
import subprocess
import threading
import time
import urllib.request

base = os.environ.get("SB_TEST_URL", "http://127.0.0.1:2096")
duration = int(os.environ.get("SB_LOAD_SECONDS", "60"))
if not 10 <= duration <= 3600:
    raise SystemExit("SB_LOAD_SECONDS must be 10..3600")
cookies = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))
request = urllib.request.Request(base + "/api/auth/login", data=json.dumps({
    "username": "admin", "password": os.environ["SB_TEST_PASSWORD"]
}).encode(), headers={"Content-Type": "application/json"})
with opener.open(request, timeout=30) as response:
    response.read()
cookie = "; ".join(c.name + "=" + c.value for c in cookies)
resources = {}
for service in ["sb-panel", "sb-core"]:
    pid = int(subprocess.check_output(["systemctl", "show", service, "--property=MainPID", "--value"], text=True))
    resources[service] = {"pid": pid, "cpu_ticks": [], "peak_rss_kib": 0, "process_lost": False}
stop = threading.Event()


def sample():
    while not stop.is_set():
        for value in resources.values():
            try:
                root = pathlib.Path("/proc") / str(value["pid"])
                fields = (root / "stat").read_text().rsplit(")", 1)[1].split()
                value["cpu_ticks"].append(int(fields[11]) + int(fields[12]))
                for line in (root / "status").read_text().splitlines():
                    if line.startswith("VmRSS:"):
                        value["peak_rss_kib"] = max(value["peak_rss_kib"], int(line.split()[1]))
            except (OSError, ValueError):
                value["process_lost"] = True
        stop.wait(0.25)


start = time.monotonic()
deadline = start + duration
monitor = threading.Thread(target=sample)
monitor.start()


def worker(index):
    latencies, errors, received = [], 0, 0
    endpoints = ["/api/overview", "/api/accounts", "/api/grants?offset=0&limit=20"]
    client = urllib.request.build_opener()
    while time.monotonic() < deadline:
        tick = time.monotonic()
        try:
            request = urllib.request.Request(base + endpoints[index % len(endpoints)], headers={"Cookie": cookie})
            with client.open(request, timeout=30) as response:
                body = response.read()
                json.loads(body)
                received += len(body)
            latencies.append((time.monotonic() - tick) * 1000)
        except Exception:
            errors += 1
        index += 1
        time.sleep(0.05)
    return latencies, errors, received


with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    results = list(pool.map(worker, range(4)))
elapsed = time.monotonic() - start
stop.set()
monitor.join()
latencies = sorted(v for batch, _, _ in results for v in batch)
for value in resources.values():
    samples = value.pop("cpu_ticks")
    value["cpu_percent_one_core"] = round(100 * (samples[-1] - samples[0]) / os.sysconf("SC_CLK_TCK") / elapsed, 2) if len(samples) > 1 else None
    del value["pid"]
report = {
    "duration_seconds": round(elapsed, 2), "workers": 4,
    "requests": len(latencies), "errors": sum(v[1] for v in results),
    "requests_per_second": round(len(latencies) / elapsed, 2),
    "response_bytes": sum(v[2] for v in results),
    "latency_ms_p50": round(statistics.median(latencies), 2) if latencies else None,
    "latency_ms_p95": round(latencies[(len(latencies) - 1) * 95 // 100], 2) if latencies else None,
    "resources": resources,
}
print(json.dumps(report, indent=2))
if report["errors"] or any(v["process_lost"] for v in resources.values()):
    raise SystemExit(1)
