#!/usr/bin/env python3
"""Stateful five-protocol management; all configuration edits are structural."""
import argparse
import copy
import ipaddress
import json
import os
import re
import secrets
import subprocess
import sys
import uuid
from pathlib import Path
from urllib.request import Request, urlopen
from store import (CORE_VERSION, COUNTRIES, LABELS, PROTOCOLS, TAGS, atomic_bytes,
                   atomic_json, available_port, certificate, check_core,
                   checked_json, derive_public, generate_reality, read_json,
                   validate_domain)
from render import (client_config, make_inbounds, mihomo_config, mihomo_node,
                    outbound, server_config, uri)

def public_ip():
    for url in ("https://api4.ipify.org", "https://ipv4.icanhazip.com"):
        try:
            with urlopen(Request(url, headers={"User-Agent": "sing-box-vpn"}), timeout=10) as response:
                value = response.read(128).decode().strip()
            if ipaddress.ip_address(value).version == 4:
                return value
        except Exception:
            pass
    raise ValueError("无法取得 VPS IPv4，请使用 --address 指定")

def new_profile(root, core, used, suffix=""):
    directory = root / "vpngate" if suffix else root
    cert, key = directory / (suffix+"cert.pem"), directory / (suffix+"private.key")
    certificate(cert, key)
    return {"ports": {p: available_port(used) for p in PROTOCOLS}, "certificate": cert.as_posix(), "key": key.as_posix(), "reality_sni": "www.apple.com", "tls_sni": "www.bing.com", "vmess_tls": False, "self_signed": True, "ws_path": "/"+secrets.token_hex(16),
            "secrets": {"uuid": {p: str(uuid.uuid4()) for p in ("vless", "vmess", "tuic")}, "passwords": {p: secrets.token_urlsafe(32) for p in ("hysteria2", "tuic", "anytls")}, "reality": generate_reality(core)}}

def import_existing(root, core, used):
    existing = read_json(root / "sb.json")
    if existing:
        used.update(i["listen_port"] for i in existing.get("inbounds",[]) if "listen_port" in i)
    profile = new_profile(root, core, used)
    if not existing:
        return profile, make_inbounds(profile), {}, None
    # Preserve an exact private backup before converting removed option formats.
    atomic_json(root / "migration-backup.json", existing)
    original = {v["type"]: v for v in existing.get("inbounds", []) if v.get("type") in PROTOCOLS}
    fresh = {v["type"]: v for v in make_inbounds(profile)}
    keep = {"type", "tag", "listen", "listen_port", "users", "tls", "transport", "ignore_client_bandwidth", "congestion_control", "padding_scheme"}
    result = []
    for p in PROTOCOLS:
        if p not in original:
            result.append(fresh[p])
            continue
        inbound = {k: copy.deepcopy(v) for k, v in original[p].items() if k in keep}
        inbound["tag"] = TAGS[p]
        profile["ports"][p] = inbound["listen_port"]
        if p == "vless":
            r = inbound["tls"]["reality"]
            profile["secrets"]["reality"] = {"private_key": r["private_key"], "public_key": derive_public(r["private_key"]), "short_id": r.get("short_id", [""])[0]}
            profile["reality_sni"] = inbound["tls"]["server_name"]
        if p == "vmess":
            profile["ws_path"] = inbound["transport"]["path"]
            profile["vmess_tls"] = inbound.get("tls", {}).get("enabled", False)
        result.append(inbound)
    routing = {k: [] for k in ("socks", "warp", "direct4", "direct6")}
    for rule in existing.get("route", {}).get("rules", []):
        target = rule.get("outbound", "")
        name = "warp" if "warp" in target or "wireguard" in target else "socks" if "socks" in target else "direct4" if target == "vps-outbound-v4" else "direct6" if target == "vps-outbound-v6" else None
        if name:
            routing[name] += [d for d in rule.get("domain_suffix", []) if d != "yg_kkk"]
    warp = next((copy.deepcopy(v) for v in existing.get("endpoints", []) if v.get("type") == "wireguard"), None)
    if not warp:
        old = next((v for v in existing.get("outbounds", []) if v.get("type") == "wireguard"), None)
        if old:
            warp = {"type": "wireguard", "tag": "warp-out", "address": old["local_address"], "private_key": old["private_key"], "peers": [{"address": old["server"], "port": old["server_port"], "public_key": old["peer_public_key"], "allowed_ips": ["0.0.0.0/0", "::/0"], "reserved": old.get("reserved", [0,0,0])}]}
    for inbound in result:
        tls = inbound.get("tls", {})
        if tls.get("enabled") and inbound["type"] != "vless":
            for name in ("certificate_path", "key_path"):
                if not Path(tls.get(name, "")).is_file():
                    raise ValueError("旧节点的证书文件缺失，已保留迁移备份，请修复："+name)
    return profile, result, routing, warp

def prepare_vpngate(root, core, app, country):
    country = country.upper()
    if not re.fullmatch(r"[A-Z]{2}", country):
        raise ValueError("请输入两位国家代码，例如 JP、KR、US")
    directory = root / "vpngate"
    settings = read_json(directory / "settings.json", {"active_country": country, "proxy_port": 7928, "profiles": {}})
    private = read_json(directory / "secrets.json", {})
    if country not in settings["profiles"]:
        used = {v["listen_port"] for v in app["inbounds"]}
        for p in settings["profiles"].values():
            used.update(p["ports"].values())
        profile = new_profile(root, core, used, country+"-")
        private[country] = profile.pop("secrets")
        private[country]["socks_user"] = "ygvg-"+country
        private[country]["socks_pass"] = secrets.token_hex(32)
        profile["country"] = country
        settings["profiles"][country] = profile
    settings["active_country"] = country
    # Only one gateway country is active. Inactive entry credentials cannot
    # authenticate against the active country's proxy, so old links fail closed.
    active = private[country]
    gateway = read_json(directory / "gateway.json", {})
    if gateway.get("country") != country:
        gateway.update(mode="sticky",fixed_host="")
    gateway.update(country=country, enabled=True, listen="127.0.0.1:"+str(settings["proxy_port"]), proxy_user=active["socks_user"], proxy_pass=active["socks_pass"], data_dir=os.environ.get("YGVG_DATA_DIR", "/var/lib/yg-vpngate"), run_dir=os.environ.get("YGVG_RUN_DIR", "/run/yg-vpngate"))
    atomic_json(directory / "secrets.json", private)
    atomic_json(directory / "settings.json", settings)
    atomic_json(directory / "gateway.json", gateway)
    config = {"log": {"level": "info", "timestamp": True}, "inbounds": [], "outbounds": [], "route": {"rules": []}}
    for cc, p in settings["profiles"].items():
        p = dict(p, secrets=private[cc])
        ins = make_inbounds(p, "vg-"+cc+"-")
        config["inbounds"] += ins
        tag = "vg-out-"+cc
        config["outbounds"].append({"type": "socks", "tag": tag, "server": "127.0.0.1", "server_port": settings["proxy_port"], "version": "5", "username": private[cc]["socks_user"], "password": private[cc]["socks_pass"]})
        config["route"]["rules"].append({"inbound": [i["tag"] for i in ins], "action": "route", "outbound": tag})
    config["route"]["rules"].append({"action": "reject"})
    checked_json(directory / "server.json", config, core)
    return settings

def exports(root, core, app):
    normal, vpn, m_normal, m_vpn, links = [], [], [], [], []
    profile = app["profile"]
    address = app["address"]
    for inbound in app["inbounds"]:
        name = LABELS[inbound["type"]]+"-普通-"+app["name"]
        n = outbound(inbound, profile, address, name)
        if inbound["type"] == "vmess" and app.get("cdn_address"):
            n["server"] = app["cdn_address"]
        normal.append(n)
        m_normal.append(mihomo_node(n, profile))
        links.append(uri(n, profile))
    if app.get("argo", {}).get("domain"):
        n = copy.deepcopy(next(v for v in normal if v["type"] == "vmess"))
        domain = app["argo"]["domain"]
        n.update(tag="VMess-Argo-普通-"+app["name"], server=app["argo"].get("cdn_address", domain), server_port=443)
        n["tls"] = {"enabled": True, "server_name": domain}
        n["transport"]["headers"] = {"Host": domain}
        p = dict(profile, tls_sni=domain, self_signed=False)
        normal.append(n)
        m_normal.append(mihomo_node(n, p))
        links.append(uri(n, p))
    settings = read_json(root / "vpngate" / "settings.json", {"profiles": {}})
    private = read_json(root / "vpngate" / "secrets.json", {})
    for cc, p in settings["profiles"].items():
        p = dict(p, secrets=private[cc])
        for inbound in make_inbounds(p, "vg-"+cc+"-"):
            n = outbound(inbound, p, address, "VPNGate-"+cc+"-"+COUNTRIES.get(cc,cc)+"-"+LABELS[inbound["type"]]+"-家宽候选")
            vpn.append(n)
            m_vpn.append(mihomo_node(n, p))
            links.append(uri(n, p))
    client = client_config(normal, vpn)
    checked_json(root / "sbox.json", client, core)
    atomic_json(root / "clmi.yaml", mihomo_config(m_normal, m_vpn))
    atomic_bytes(root / "jhsub.txt", ("\n".join(links)+"\n").encode())
    atomic_bytes(root / "jhsub-base64.txt", base64_encode("\n".join(links)))
    atomic_json(root / "share.json", {"links": links, "normal": [n["tag"] for n in normal], "vpngate": [n["tag"] for n in vpn]})
    return links

def base64_encode(text):
    import base64
    return base64.b64encode(text.encode())+b"\n"

def commit_app(root, core, app):
    config = server_config(app["inbounds"], app.get("routing"), app.get("warp"), app.get("socks_port",40000))
    checked_json(root / "sb.json", config, core)
    atomic_json(root / "app.json", app)
    exports(root, core, app)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default="/etc/s-box")
    parser.add_argument("--core")
    sub = parser.add_subparsers(dest="command", required=True)
    init = sub.add_parser("init");init.add_argument("--address");init.add_argument("--name",default="VPS")
    sub.add_parser("export");sub.add_parser("show");sub.add_parser("links")
    port = sub.add_parser("port");port.add_argument("protocol",choices=PROTOCOLS);port.add_argument("port",type=int)
    reset = sub.add_parser("reset");reset.add_argument("protocol",choices=PROTOCOLS+("all",))
    routing = sub.add_parser("route");routing.add_argument("channel",choices=("socks","warp","direct4","direct6"));routing.add_argument("domains",nargs="*")
    socks = sub.add_parser("socks");socks.add_argument("port",type=int)
    address = sub.add_parser("address");address.add_argument("address")
    sni = sub.add_parser("sni");sni.add_argument("domain")
    ws = sub.add_parser("ws");ws.add_argument("path")
    tls = sub.add_parser("tls");tls.add_argument("protocol",choices=PROTOCOLS+("all",));tls.add_argument("cert");tls.add_argument("key");tls.add_argument("domain")
    vg = sub.add_parser("vpngate");vg.add_argument("country")
    sub.add_parser("vpngate-remove")
    cdn = sub.add_parser("cdn");cdn.add_argument("address",nargs="?",default="")
    argo = sub.add_parser("argo");argo.add_argument("domain",nargs="?",default="")
    warp = sub.add_parser("warp");warp.add_argument("file")
    hop = sub.add_parser("hopping");hop.add_argument("ports",nargs="*")
    args = parser.parse_args();root=Path(args.root);core=Path(args.core) if args.core else root/"sing-box"
    app = read_json(root / "app.json")
    if args.command == "init":
        check_core(core)
        if app:
            commit_app(root,core,app);return
        used=set();profile,ins,routing,warp=import_existing(root,core,used)
        address = args.address or (root/"server_ipcl.log").read_text().strip() if (root/"server_ipcl.log").exists() else args.address
        address = address or public_ip()
        app={"version": CORE_VERSION,"name":args.name,"address":address,"profile":profile,"inbounds":ins,"routing":routing,"warp":warp,"socks_port":40000}
        commit_app(root,core,app);return
    if not app:
        raise ValueError("请先安装或迁移普通节点")
    cmd=args.command
    if cmd=="export":exports(root,core,app);return
    if cmd=="show":
        print(json.dumps({"core":CORE_VERSION,"address":app["address"],"protocols":{i["type"]:i["listen_port"] for i in app["inbounds"]},"routing":app.get("routing",{})},ensure_ascii=False,indent=2));return
    if cmd=="links":
        for link in exports(root,core,app):print(link)
        return
    if cmd=="vpngate":prepare_vpngate(root,core,app,args.country);exports(root,core,app);return
    if cmd=="vpngate-remove":
        settings=root/"vpngate"/"settings.json"
        if settings.exists():settings.unlink()
        exports(root,core,app);return
    if cmd=="port":
        if not 1<=args.port<=65535:raise ValueError("端口必须为 1-65535")
        old_port=next(i["listen_port"] for i in app["inbounds"] if i["type"]==args.protocol)
        if args.port!=old_port:
            import socket
            probes=[]
            try:
                for kind in (socket.SOCK_STREAM,socket.SOCK_DGRAM):
                    s=socket.socket(socket.AF_INET,kind);probes.append(s);s.bind(("0.0.0.0",args.port))
            finally:
                for s in probes:s.close()
        if any(i["listen_port"]==args.port and i["type"]!=args.protocol for i in app["inbounds"]):raise ValueError("端口已被其他协议使用")
        for i in app["inbounds"]:
            if i["type"]==args.protocol:i["listen_port"]=args.port
        app["profile"]["ports"][args.protocol]=args.port
    elif cmd=="reset":
        for i in app["inbounds"]:
            if args.protocol not in ("all",i["type"]):continue
            for user in i["users"]:
                if "uuid" in user:user["uuid"]=str(uuid.uuid4())
                if "password" in user:user["password"]=secrets.token_urlsafe(32)
    elif cmd=="route":app.setdefault("routing",{})[args.channel]=[validate_domain(d.lstrip(".")) for d in args.domains]
    elif cmd=="socks":
        if not 1<=args.port<=65535:raise ValueError("端口错误")
        app["socks_port"]=args.port
    elif cmd=="address":
        try:ipaddress.ip_address(args.address)
        except ValueError:validate_domain(args.address)
        app["address"]=args.address
    elif cmd=="sni":
        validate_domain(args.domain)
        for i in app["inbounds"]:
            if i["type"]=="vless":i["tls"]["server_name"]=args.domain;i["tls"]["reality"]["handshake"]["server"]=args.domain
        app["profile"]["reality_sni"]=args.domain
    elif cmd=="ws":
        if not args.path or len(args.path)>255 or "\n" in args.path:raise ValueError("路径格式错误")
        for i in app["inbounds"]:
            if i["type"]=="vmess":i["transport"]["path"]=args.path
        app["profile"]["ws_path"]=args.path
    elif cmd=="tls":
        validate_domain(args.domain);certificate(args.cert,args.key,args.domain)
        for i in app["inbounds"]:
            if i["type"]=="vless" or args.protocol not in ("all",i["type"]):continue
            i["tls"].update(enabled=True,certificate_path=str(Path(args.cert).resolve()),key_path=str(Path(args.key).resolve()),server_name=args.domain)
        app["profile"].update(tls_sni=args.domain,self_signed=False)
    elif cmd=="cdn":app["cdn_address"]=args.address
    elif cmd=="argo":
        if args.domain:validate_domain(args.domain)
        app["argo"]={"domain":args.domain}
    elif cmd=="warp":
        w=read_json(args.file)
        if w.get("type")!="wireguard" or not w.get("peers") or not w.get("private_key"):raise ValueError("请输入新版 WireGuard endpoint JSON")
        app["warp"]=w
    elif cmd=="hopping":
        for value in args.ports:
            if not re.fullmatch(r"\d+(?::\d+)?",value):raise ValueError("端口跳跃使用 20000:20100 或单端口格式")
            numbers=list(map(int,value.split(":")))
            if any(not 1<=v<=65535 for v in numbers) or numbers[0]>numbers[-1]:raise ValueError("端口范围错误")
        app["profile"]["hopping"]=args.ports
    commit_app(root,core,app)

if __name__=="__main__":
    try:main()
    except (ValueError,KeyError,OSError,subprocess.SubprocessError) as error:
        print("配置操作失败："+str(error),file=sys.stderr);sys.exit(1)
