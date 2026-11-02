#!/usr/bin/env python3
"""Create a self-contained, reproducible-source release bundle and checksums."""
import hashlib
import json
import os
import subprocess
import tarfile
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
VERSIONS=json.loads((ROOT/"versions.json").read_text())
DEST=ROOT/".build"/"release"
DEST.mkdir(parents=True,exist_ok=True)
go=os.environ.get("GO_BINARY","go")
version=subprocess.run([go,"version"],capture_output=True,text=True,check=True).stdout
if "go"+VERSIONS["go"]+" " not in version:raise RuntimeError("build requires Go "+VERSIONS["go"])
for arch in ("amd64","arm64"):
    binary=DEST/".bin"/("linux-"+arch)/"yg-vpngate";binary.parent.mkdir(parents=True,exist_ok=True)
    env=dict(os.environ,GOOS="linux",GOARCH=arch,CGO_ENABLED="0")
    subprocess.run([go,"build","-trimpath","-ldflags","-s -w -X main.version="+VERSIONS["release"].lstrip("v"),"-o",str(binary),"."],cwd=ROOT/"addons"/"vpngate"/"gateway",env=env,check=True)
    binary.chmod(0o755)
archive=DEST/("sing-box-vpn-"+VERSIONS["release"]+".tar.gz")
members=("sb.sh","install.sh","lib","addons","versions.json","LICENSE","NOTICE.md","README.md","VPNGate-实施方案.md","docs","tests","scripts",".github")
with tarfile.open(archive,"w:gz") as tar:
    def filter_member(info):
        if "__pycache__" in info.name or info.name.endswith(".pyc"):return None
        if info.issym() or info.islnk():raise RuntimeError("release must not contain links")
        info.uid=info.gid=0;info.uname=info.gname="root"
        if info.name.endswith(".sh") or info.name.endswith("/yg-vpngate"):info.mode=0o755
        return info
    for member in members:
        path=ROOT/member
        if path.exists():tar.add(path,arcname="sing-box-vpn/"+member,filter=filter_member)
    tar.add(DEST/".bin",arcname="sing-box-vpn/.bin",filter=filter_member)
digest=hashlib.sha256(archive.read_bytes()).hexdigest()
(DEST/"SHA256SUMS").write_text(digest+"  "+archive.name+"\n")
print(archive.name+" "+digest)
