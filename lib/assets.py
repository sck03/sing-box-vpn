#!/usr/bin/env python3
"""Download optional release binaries, requiring the publisher's SHA256."""
import argparse
import hashlib
import json
import os
import tempfile
from pathlib import Path
from urllib.request import Request, urlopen

def cloudflared(arch,destination):
    req=Request("https://api.github.com/repos/cloudflare/cloudflared/releases/latest",headers={"User-Agent":"sing-box-vpn"})
    with urlopen(req,timeout=20) as response:release=json.load(response)
    name="cloudflared-linux-"+arch
    asset=next(a for a in release["assets"] if a["name"]==name)
    digest=asset.get("digest","")
    if not digest.startswith("sha256:"):raise ValueError("发行资产未提供 SHA256，停止下载")
    destination=Path(destination);destination.parent.mkdir(parents=True,exist_ok=True)
    fd,tmp=tempfile.mkstemp(prefix=".asset-",dir=destination.parent)
    try:
        with os.fdopen(fd,"wb") as out,urlopen(Request(asset["browser_download_url"],headers={"User-Agent":"sing-box-vpn"}),timeout=90) as response:
            while True:
                chunk=response.read(1<<20)
                if not chunk:break
                out.write(chunk)
        if hashlib.sha256(Path(tmp).read_bytes()).hexdigest()!=digest[7:]:raise ValueError("SHA256 不匹配")
        os.chmod(tmp,0o755);os.replace(tmp,destination)
    finally:
        if os.path.exists(tmp):os.unlink(tmp)

if __name__=="__main__":
    p=argparse.ArgumentParser();p.add_argument("arch",choices=("amd64","arm64"));p.add_argument("destination");a=p.parse_args();cloudflared(a.arch,a.destination)
