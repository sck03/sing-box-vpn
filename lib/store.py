"""Private state and atomic sing-box configuration updates."""
import base64
import copy
import hashlib
import json
import os
import re
import secrets
import socket
import subprocess
import tempfile
from pathlib import Path

CORE_VERSION = "1.14.4"
MIN_CORE_VERSION = (1, 14, 0)
PROTOCOLS = ("vless", "vmess", "hysteria2", "tuic", "anytls")
TAGS = {"vless": "vless-sb", "vmess": "vmess-sb", "hysteria2": "hy2-sb", "tuic": "tuic5-sb", "anytls": "anytls-sb"}
LABELS = {"vless": "VLESS-Reality", "vmess": "VMess-WS", "hysteria2": "Hysteria-2", "tuic": "TUIC-v5", "anytls": "AnyTLS"}
COUNTRIES = {"JP": "日本", "KR": "韩国", "US": "美国", "TW": "台湾", "HK": "香港", "SG": "新加坡", "TH": "泰国", "VN": "越南", "DE": "德国", "GB": "英国", "CA": "加拿大"}

def read_json(path, default=None):
    path = Path(path)
    if not path.exists():
        return copy.deepcopy(default)
    return json.loads(path.read_text(encoding="utf-8"))

def atomic_json(path, obj, mode=0o600):
    atomic_bytes(path, (json.dumps(obj, ensure_ascii=False, indent=2) + "\n").encode(), mode)

def atomic_bytes(path, data, mode=0o600):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".write-", dir=path.parent)
    try:
        os.chmod(name, mode)
        with os.fdopen(fd, "wb") as out:
            out.write(data)
            out.flush()
            os.fsync(out.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)

def check_core(core):
    result = subprocess.run([str(core), "version"], capture_output=True, text=True, check=True)
    match = re.search(r"sing-box version (\d+\.\d+\.\d+)([^\s]*)", result.stdout)
    if not match or match.group(2).startswith("-"):
        raise ValueError("需要 sing-box 正式稳定版")
    version = match.group(1)
    if tuple(map(int,version.split("."))) < MIN_CORE_VERSION:
        raise ValueError("当前配置要求 sing-box 1.14.0 或更新的稳定版")
    return version

def checked_json(path, obj, core):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".check-", suffix=".json", dir=path.parent)
    try:
        os.chmod(name, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as out:
            json.dump(obj, out, ensure_ascii=False, indent=2)
            out.flush()
            os.fsync(out.fileno())
        result = subprocess.run([str(core), "check", "-c", name], capture_output=True, text=True, timeout=30)
        if result.returncode:
            raise ValueError("配置检查失败：" + result.stderr.strip())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)

def generate_reality(core):
    result = subprocess.run([str(core), "generate", "reality-keypair"], capture_output=True, text=True, check=True)
    return {"private_key": re.search(r"PrivateKey:\s*(\S+)", result.stdout).group(1),
            "public_key": re.search(r"PublicKey:\s*(\S+)", result.stdout).group(1), "short_id": secrets.token_hex(4)}

def derive_public(private):
    # RFC 7748 Montgomery ladder, used only to recover an existing public key.
    raw = bytearray(base64.urlsafe_b64decode(private + "=" * (-len(private) % 4)))
    if len(raw) != 32:
        raise ValueError("旧 Reality 私钥格式错误")
    raw[0] &= 248
    raw[31] = (raw[31] & 127) | 64
    scalar = int.from_bytes(raw, "little")
    prime = 2 ** 255 - 19
    x1, x2, z2, x3, z3, swap = 9, 1, 0, 9, 1, 0
    for bit in range(254, -1, -1):
        current = (scalar >> bit) & 1
        swap ^= current
        if swap:
            x2, x3, z2, z3 = x3, x2, z3, z2
        swap = current
        a, b = (x2 + z2) % prime, (x2 - z2) % prime
        aa, bb = a*a % prime, b*b % prime
        e = (aa - bb) % prime
        c, d = (x3 + z3) % prime, (x3 - z3) % prime
        da, cb = d*a % prime, c*b % prime
        x3, z3 = (da+cb)**2 % prime, x1*(da-cb)**2 % prime
        x2, z2 = aa*bb % prime, e*(aa+121665*e) % prime
    if swap:
        x2, z2 = x3, z3
    public = (x2 * pow(z2, prime-2, prime) % prime).to_bytes(32, "little")
    return base64.urlsafe_b64encode(public).decode().rstrip("=")

def certificate(cert, key, domain="www.bing.com"):
    cert, key = Path(cert), Path(key)
    if cert.exists() != key.exists():
        raise ValueError("证书和私钥缺少其中一项，请修复后重试")
    if not cert.exists():
        cert.parent.mkdir(parents=True, exist_ok=True)
        result = subprocess.run(["openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes", "-days", "3650", "-keyout", str(key), "-out", str(cert), "-subj", "/CN="+domain, "-addext", "subjectAltName=DNS:"+domain], capture_output=True)
        if result.returncode:
            raise ValueError("证书生成失败")
        os.chmod(key, 0o600)
        os.chmod(cert, 0o600)
    subprocess.run(["openssl", "x509", "-in", str(cert), "-noout"], capture_output=True, check=True)

def certificate_hash(cert):
    der = subprocess.run(["openssl", "x509", "-in", str(cert), "-outform", "DER"], capture_output=True, check=True).stdout
    return hashlib.sha256(der).hexdigest()

def certificate_pin(cert):
    pem = subprocess.run(["openssl", "x509", "-in", str(cert), "-pubkey", "-noout"], capture_output=True, check=True).stdout
    der = subprocess.run(["openssl", "pkey", "-pubin", "-outform", "DER"], input=pem, capture_output=True, check=True).stdout
    return base64.b64encode(hashlib.sha256(der).digest()).decode()

def available_port(used):
    for _ in range(200):
        port = 20000 + secrets.randbelow(40000)
        if port in used:
            continue
        sockets = []
        try:
            for kind in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
                s = socket.socket(socket.AF_INET, kind)
                sockets.append(s)
                s.bind(("0.0.0.0", port))
            used.add(port)
            return port
        except OSError:
            pass
        finally:
            for s in sockets:
                s.close()
    raise ValueError("没有可用入口端口")

def validate_domain(value):
    if not re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?", value):
        raise ValueError("域名格式错误")
    return value
