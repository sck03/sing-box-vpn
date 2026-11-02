"""sing-box 1.14.4 and Mihomo renderers, adapted from sing-box-yg."""
import base64
import copy
import json
from urllib.parse import quote, urlencode
from store import LABELS, PROTOCOLS, certificate_hash, certificate_pin

def server_config(inbounds, routing=None, warp=None, socks_port=40000):
    outbounds = [{"type": "direct", "tag": "direct"}, {"type": "socks", "tag": "socks-out", "server": "127.0.0.1", "server_port": socks_port, "version": "5"}]
    rules = [{"action": "sniff"}]
    routing = routing or {}
    for name in ("socks", "warp", "direct4", "direct6"):
        domains = routing.get(name, [])
        if not domains:
            continue
        if name.startswith("direct"):
            rules.append({"domain_suffix": domains, "action": "resolve", "strategy": "prefer_ipv4" if name == "direct4" else "prefer_ipv6"})
        if name == "warp" and not warp:
            raise ValueError("请先配置 WARP endpoint")
        rules.append({"domain_suffix": domains, "action": "route", "outbound": {"socks": "socks-out", "warp": "warp-out"}.get(name, "direct")})
    result = {"log": {"level": "info", "timestamp": True}, "inbounds": inbounds, "outbounds": outbounds, "route": {"rules": rules, "final": "direct"}}
    if warp:
        result["endpoints"] = [dict(copy.deepcopy(warp), type="wireguard", tag="warp-out")]
    return result

def make_inbounds(profile, prefix=""):
    p, s = profile["ports"], profile["secrets"]
    tls = {"enabled": True, "certificate_path": profile["certificate"], "key_path": profile["key"]}
    uid = s["uuid"]
    return [
        {"type": "vless", "tag": prefix+"vless-sb", "listen": "::", "listen_port": p["vless"], "users": [{"uuid": uid["vless"], "flow": "xtls-rprx-vision"}], "tls": {"enabled": True, "server_name": profile["reality_sni"], "reality": {"enabled": True, "handshake": {"server": profile.get("handshake_host", profile["reality_sni"]), "server_port": profile.get("handshake_port", 443)}, "private_key": s["reality"]["private_key"], "short_id": [s["reality"]["short_id"]]}}},
        {"type": "vmess", "tag": prefix+"vmess-sb", "listen": "::", "listen_port": p["vmess"], "users": [{"uuid": uid["vmess"], "alterId": 0}], "transport": {"type": "ws", "path": profile["ws_path"], "max_early_data": 2048, "early_data_header_name": "Sec-WebSocket-Protocol"}, "tls": dict(tls, enabled=profile.get("vmess_tls", False), server_name=profile["tls_sni"])},
        {"type": "hysteria2", "tag": prefix+"hy2-sb", "listen": "::", "listen_port": p["hysteria2"], "users": [{"password": s["passwords"]["hysteria2"]}], "ignore_client_bandwidth": False, "tls": dict(tls, alpn=["h3"])},
        {"type": "tuic", "tag": prefix+"tuic5-sb", "listen": "::", "listen_port": p["tuic"], "users": [{"uuid": uid["tuic"], "password": s["passwords"]["tuic"]}], "congestion_control": "bbr", "tls": dict(tls, alpn=["h3"])},
        {"type": "anytls", "tag": prefix+"anytls-sb", "listen": "::", "listen_port": p["anytls"], "users": [{"password": s["passwords"]["anytls"]}], "tls": tls},
    ]

def outbound(inbound, profile, address, name):
    kind = inbound["type"]
    result = {"type": kind, "tag": name, "server": address, "server_port": inbound["listen_port"]}
    user = inbound["users"][0]
    if "uuid" in user:
        result["uuid"] = user["uuid"]
    if "password" in user:
        result["password"] = user["password"]
    if kind == "vless":
        result.update(flow="xtls-rprx-vision", tls={"enabled": True, "server_name": inbound["tls"]["server_name"], "utls": {"enabled": True, "fingerprint": "chrome"}, "reality": {"enabled": True, "public_key": profile["secrets"]["reality"]["public_key"], "short_id": profile["secrets"]["reality"]["short_id"]}})
    else:
        server_tls = inbound.get("tls", {})
        result["tls"] = {"enabled": server_tls.get("enabled", False), "server_name": server_tls.get("server_name", profile["tls_sni"])}
        if result["tls"]["enabled"]:
            if profile.get("self_signed", True):
                result["tls"]["certificate_public_key_sha256"] = [certificate_pin(server_tls["certificate_path"])]
            if kind in ("hysteria2", "tuic"):
                result["tls"]["alpn"] = ["h3"]
        if kind == "vmess":
            result.update(security="auto", transport={"type": "ws", "path": inbound["transport"]["path"], "headers": {"Host": profile["tls_sni"]}})
        elif kind == "tuic":
            result.update(congestion_control="bbr", udp_relay_mode="native", zero_rtt_handshake=False, heartbeat="10s")
        elif kind == "anytls":
            result.update(idle_session_check_interval="30s", idle_session_timeout="30s", min_idle_session=5)
        elif kind == "hysteria2" and profile.get("hopping"):
            result["server_ports"] = [value if ":" in value else value+":"+value for value in profile["hopping"]]
    return result

def uri(node, profile):
    kind, address, port = node["type"], node["server"], node["server_port"]
    endpoint = ("["+address+"]" if ":" in address else address) + ":" + str(port)
    name = quote(node["tag"], safe="")
    tls = node.get("tls", {})
    params = {"sni": tls.get("server_name", profile["tls_sni"])}
    insecure = int(profile.get("self_signed", True))
    if kind == "vless":
        params.update(encryption="none", flow="xtls-rprx-vision", security="reality", fp="chrome", pbk=tls["reality"]["public_key"], sid=tls["reality"]["short_id"], type="tcp")
        return "vless://"+node["uuid"]+"@"+endpoint+"?"+urlencode(params)+"#"+name
    if kind == "vmess":
        value = {"v": "2", "ps": node["tag"], "add": address, "port": str(port), "id": node["uuid"], "aid": "0", "scy": "auto", "net": "ws", "type": "none", "host": profile["tls_sni"], "path": node["transport"]["path"], "tls": "tls" if tls["enabled"] else "", "sni": tls.get("server_name", "")}
        return "vmess://"+base64.b64encode(json.dumps(value, separators=(",", ":")).encode()).decode()
    if kind == "hysteria2":
        params.update(alpn="h3", insecure=insecure)
        if not insecure:
            params["pinSHA256"] = certificate_hash(profile["certificate"])
        if profile.get("hopping"):
            params["mport"] = ",".join(profile["hopping"]).replace(":", "-")
        return "hysteria2://"+quote(node["password"], safe="")+"@"+endpoint+"?"+urlencode(params)+"#"+name
    if kind == "tuic":
        params.update(congestion_control="bbr", udp_relay_mode="native", alpn="h3", allow_insecure=insecure)
        return "tuic://"+node["uuid"]+":"+quote(node["password"], safe="")+"@"+endpoint+"?"+urlencode(params)+"#"+name
    params["insecure"] = insecure
    return "anytls://"+quote(node["password"], safe="")+"@"+endpoint+"?"+urlencode(params)+"#"+name

def client_config(normal, vpn):
    ordinary = [v["tag"] for v in normal]
    selectors = [{"type": "urltest", "tag": "auto", "outbounds": ordinary, "url": "https://www.gstatic.com/generate_204", "interval": "10m", "tolerance": 30}, {"type": "selector", "tag": "proxy", "outbounds": ["auto"]+ordinary+(["vpngate"] if vpn else []), "default": "auto"}]
    if vpn:
        selectors.append({"type": "selector", "tag": "vpngate", "outbounds": [v["tag"] for v in vpn]})
    return {"log": {"level": "info"}, "http_clients": [{"tag": "rule-set-direct"}],
            "dns": {"servers": [{"type": "fakeip", "tag": "fakeip", "inet4_range": "198.18.0.0/15"}, {"type": "udp", "tag": "dns-cn", "server": "223.5.5.5"}, {"type": "https", "tag": "dns-proxy", "server": "1.1.1.1", "detour": "proxy"}], "rules": [{"rule_set": ["cn-domains"], "action": "route", "server": "dns-cn"}, {"query_type": ["A", "AAAA"], "action": "route", "server": "fakeip"}], "final": "dns-proxy", "strategy": "ipv4_only", "reverse_mapping": True},
            "inbounds": [{"type": "tun", "tag": "tun-in", "address": ["172.19.0.1/30"], "auto_route": True, "strict_route": True, "mtu": 1420}],
            "route": {"default_http_client": "rule-set-direct", "default_domain_resolver": "dns-cn", "auto_detect_interface": True, "rule_set": [{"type": "remote", "tag": "cn-domains", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs"}, {"type": "remote", "tag": "cn-ips", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs"}], "rules": [{"port": 53, "action": "hijack-dns"}, {"clash_mode": "Global", "action": "route", "outbound": "proxy"}, {"clash_mode": "Direct", "action": "route", "outbound": "direct"}, {"ip_is_private": True, "action": "route", "outbound": "direct"}, {"rule_set": ["cn-domains", "cn-ips"], "action": "route", "outbound": "direct"}], "final": "proxy"},
            "experimental": {"cache_file": {"enabled": True, "store_dns": True}, "clash_api": {"external_controller": "127.0.0.1:9090", "default_mode": "Rule"}},
            "outbounds": normal+vpn+selectors+[{"type": "direct", "tag": "direct"}]}

def mihomo_node(node, profile):
    kind = node["type"]
    p = {"name": node["tag"], "type": kind, "server": node["server"], "port": node["server_port"], "udp": True}
    for key in ("uuid", "password"):
        if key in node:
            p[key] = node[key]
    tls = node.get("tls", {})
    if kind == "vless":
        p.update(network="tcp", tls=True, flow="xtls-rprx-vision", servername=tls["server_name"], **{"client-fingerprint": "chrome", "reality-opts": {"public-key": tls["reality"]["public_key"], "short-id": tls["reality"]["short_id"]}})
    elif kind == "vmess":
        p.update(network="ws", tls=tls["enabled"], cipher="auto", alterId=0, servername=tls.get("server_name", ""), **{"ws-opts": {"path": node["transport"]["path"], "headers": {"Host": profile["tls_sni"]}}, "skip-cert-verify": profile.get("self_signed", True)})
    else:
        p.update(sni=tls.get("server_name", profile["tls_sni"]), **{"skip-cert-verify": profile.get("self_signed", True)})
        if kind in ("hysteria2", "tuic"):
            p["alpn"] = ["h3"]
        if kind == "tuic":
            p.update(**{"congestion-controller": "bbr", "udp-relay-mode": "native"})
        if kind == "hysteria2" and profile.get("hopping"):
            p["ports"] = ",".join(profile["hopping"]).replace(":", "-")
    return p

def mihomo_config(ordinary, vpn):
    names = [n["name"] for n in ordinary]
    groups = [{"name": "节点选择", "type": "select", "proxies": ["自动选择"]+names+(["临时家宽"] if vpn else [])+["DIRECT"]}, {"name": "自动选择", "type": "url-test", "proxies": names, "url": "https://www.gstatic.com/generate_204", "interval": 600}]
    if vpn:
        groups.append({"name": "临时家宽", "type": "select", "proxies": [n["name"] for n in vpn]})
    return {"mixed-port": 7890, "allow-lan": False, "mode": "rule", "log-level": "info", "dns": {"enable": True, "ipv6": False, "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16", "respect-rules": True, "default-nameserver": ["223.5.5.5"], "nameserver": ["https://1.1.1.1/dns-query"], "proxy-server-nameserver": ["https://223.5.5.5/dns-query"]}, "proxies": ordinary+vpn, "proxy-groups": groups, "rules": ["IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,192.168.0.0/16,DIRECT,no-resolve", "GEOIP,CN,DIRECT", "MATCH,节点选择"]}
