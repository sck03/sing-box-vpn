#!/usr/bin/env python3
"""Only manage this project's INPUT and Hysteria2 port-hopping chains."""
import argparse
import json
import subprocess
from pathlib import Path
from store import read_json

def run(binary, *args, required=True):
    result = subprocess.run([binary, "-w", "5", *args], capture_output=True, text=True)
    if required and result.returncode:
        raise RuntimeError("防火墙操作失败："+result.stderr.strip())
    return result.returncode == 0

def sync(root, remove=False):
    root = Path(root)
    marker = root / "firewall.owner"
    owned = marker.exists()
    for binary in ("iptables", "ip6tables"):
        if not owned and run(binary, "-S", "SBVPN_INPUT", required=False):
            raise RuntimeError("SBVPN_INPUT 已存在且无本项目所有权记录")
        if remove:
            if not owned:
                continue
            while run(binary, "-C", "INPUT", "-j", "SBVPN_INPUT", required=False):
                run(binary, "-D", "INPUT", "-j", "SBVPN_INPUT")
            run(binary, "-F", "SBVPN_INPUT", required=False)
            run(binary, "-X", "SBVPN_INPUT", required=False)
            continue
        if not run(binary, "-S", "SBVPN_INPUT", required=False):
            if not run(binary, "-N", "SBVPN_INPUT", required=False):
                if binary == "ip6tables":
                    continue
                raise RuntimeError("无法创建 IPv4 防火墙链")
        run(binary, "-F", "SBVPN_INPUT")
        app = read_json(root / "app.json", {})
        inputs = app.get("inbounds", [])
        vg = read_json(root / "vpngate" / "server.json", {})
        inputs += vg.get("inbounds", [])
        for i in inputs:
            protocol = "udp" if i["type"] in ("hysteria2", "tuic") else "tcp"
            run(binary, "-A", "SBVPN_INPUT", "-p", protocol, "--dport", str(i["listen_port"]), "-j", "ACCEPT")
        subscription = read_json(root / "subscription.json", {})
        if subscription.get("enabled"):
            run(binary, "-A", "SBVPN_INPUT", "-p", "tcp", "--dport", str(subscription["port"]), "-j", "ACCEPT")
        for port in app.get("profile", {}).get("hopping", []):
            run(binary, "-A", "SBVPN_INPUT", "-p", "udp", "--dport", port, "-j", "ACCEPT")
        if not run(binary, "-C", "INPUT", "-j", "SBVPN_INPUT", required=False):
            run(binary, "-I", "INPUT", "1", "-j", "SBVPN_INPUT")
    # IPv4 NAT hopping is separate from the gateway's namespaced rules.
    if owned:
        while run("iptables", "-t", "nat", "-C", "PREROUTING", "-j", "SBVPN_HY2", required=False):
            run("iptables", "-t", "nat", "-D", "PREROUTING", "-j", "SBVPN_HY2")
        run("iptables", "-t", "nat", "-F", "SBVPN_HY2", required=False)
        run("iptables", "-t", "nat", "-X", "SBVPN_HY2", required=False)
    elif run("iptables", "-t", "nat", "-S", "SBVPN_HY2", required=False):
        raise RuntimeError("SBVPN_HY2 已存在且无所有权记录")
    if remove:
        marker.unlink(missing_ok=True)
        return
    app = read_json(root / "app.json")
    hopping = app.get("profile", {}).get("hopping", [])
    if hopping:
        run("iptables", "-t", "nat", "-N", "SBVPN_HY2")
        target = next(i["listen_port"] for i in app["inbounds"] if i["type"] == "hysteria2")
        for port in hopping:
            run("iptables", "-t", "nat", "-A", "SBVPN_HY2", "-p", "udp", "--dport", port, "-j", "REDIRECT", "--to-ports", str(target))
        run("iptables", "-t", "nat", "-I", "PREROUTING", "1", "-j", "SBVPN_HY2")
    marker.write_text("sck03/sing-box-vpn\n")

if __name__ == "__main__":
    p=argparse.ArgumentParser();p.add_argument("--root",default="/etc/s-box");p.add_argument("--remove",action="store_true");a=p.parse_args();sync(a.root,a.remove)
