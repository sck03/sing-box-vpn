#!/usr/bin/env python3
"""Official stable-core updates: verify, stage, check configs, restart, rollback."""
import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
from contextlib import contextmanager
from pathlib import Path
from urllib.request import Request, urlopen
from store import MIN_CORE_VERSION, check_core

API = "https://api.github.com/repos/SagerNet/sing-box/releases"
PROJECT = Path(__file__).resolve().parents[1]
CONFIGS = ("sb.json", "vpngate/server.json", "sbox.json")

@contextmanager
def core_lock(root):
    root=Path(root);root.mkdir(parents=True,exist_ok=True)
    stream=(root/".core-update.lock").open("a+b")
    try:
        if os.name=="posix":
            import fcntl
            try:fcntl.flock(stream.fileno(),fcntl.LOCK_EX|fcntl.LOCK_NB)
            except BlockingIOError:raise ValueError("另一个核心更新正在进行")
        else:
            import msvcrt
            if stream.seek(0,2)==0:stream.write(b"0");stream.flush()
            stream.seek(0)
            try:msvcrt.locking(stream.fileno(),msvcrt.LK_NBLCK,1)
            except OSError:raise ValueError("另一个核心更新正在进行")
        yield
    finally:
        stream.close()

def stable_version(value):
    value=value.removeprefix("v")
    if not re.fullmatch(r"\d+\.\d+\.\d+",value):
        raise ValueError("请输入正式稳定版，例如 1.14.4")
    if tuple(map(int,value.split(".")))<MIN_CORE_VERSION:
        raise ValueError("不支持低于 1.14.0 的旧核心")
    return value

def architecture():
    value=platform.machine().lower()
    if value in ("x86_64","amd64"):return "amd64"
    if value in ("aarch64","arm64"):return "arm64"
    raise ValueError("仅支持 Linux amd64/arm64")

def request(url):
    return urlopen(Request(url,headers={"User-Agent":"sing-box-vpn-core-updater","Accept":"application/vnd.github+json"}),timeout=60)

def select_asset(release,arch):
    if release.get("draft") or release.get("prerelease"):
        raise ValueError("该发行不是正式稳定版")
    version=stable_version(release["tag_name"])
    name=f"sing-box-{version}-linux-{arch}.tar.gz"
    asset=next((a for a in release.get("assets",[]) if a["name"]==name),None)
    if not asset:raise ValueError("官方发行缺少本机架构二进制")
    expected_url=f"https://github.com/SagerNet/sing-box/releases/download/v{version}/{name}"
    if asset.get("browser_download_url")!=expected_url:
        raise ValueError("发行下载地址不是预期官方地址")
    digest=asset.get("digest","")
    if not digest.startswith("sha256:"):
        baseline=json.loads((PROJECT/"versions.json").read_text())["sing_box"]
        digest="sha256:"+baseline.get(f"linux_{arch}_sha256","") if version==baseline.get("tested_version",baseline.get("version")) else ""
    if not re.fullmatch(r"sha256:[0-9a-fA-F]{64}",digest):
        raise ValueError("官方发行未提供可验证 SHA256")
    return {"version":version,"name":name,"url":expected_url,"sha256":digest[7:].lower()}

def release_asset(version=None,arch=None):
    endpoint=API+("/tags/v"+stable_version(version) if version else "/latest")
    with request(endpoint) as response:
        release=json.loads(response.read(4<<20))
    return select_asset(release,arch or architecture())

def download(asset,destination):
    destination=Path(destination);destination.parent.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".core-download-",dir=destination.parent) as td:
        archive=Path(td)/"core.tar.gz";digest=hashlib.sha256();size=0
        with request(asset["url"]) as response,archive.open("wb") as out:
            while True:
                data=response.read(1<<20)
                if not data:break
                size+=len(data)
                if size>256<<20:raise ValueError("核心归档超过大小限制")
                digest.update(data);out.write(data)
        if digest.hexdigest()!=asset["sha256"]:raise ValueError("核心归档 SHA256 不匹配")
        member_name=f"sing-box-{asset['version']}-linux-{architecture()}/sing-box"
        with tarfile.open(archive,"r:gz") as tar:
            member=tar.getmember(member_name)
            if not member.isfile() or member.size>256<<20:
                raise ValueError("归档中的核心文件无效")
            candidate=Path(td)/"sing-box"
            with tar.extractfile(member) as source,candidate.open("wb") as out:shutil.copyfileobj(source,out)
        candidate.chmod(0o755)
        if check_core(candidate)!=asset["version"]:raise ValueError("下载核心与发行版本不符")
        os.replace(candidate,destination)
    return asset["version"]

def config_checks(root,candidate):
    snapshots={}
    for name in CONFIGS:
        path=Path(root)/name
        if not path.exists():continue
        before=path.read_bytes()
        result=subprocess.run([str(candidate),"check","-c",str(path)],capture_output=True,text=True,timeout=60)
        if result.returncode:
            raise ValueError(f"新核心不兼容 {name}，保留当前核心："+result.stderr.strip())
        snapshots[name]=hashlib.sha256(before).hexdigest()
    for name,digest in snapshots.items():
        if hashlib.sha256((Path(root)/name).read_bytes()).hexdigest()!=digest:
            raise ValueError("校验期间配置发生变化，请重试更新")

class Services:
    def active(self):
        return [name for name in ("sing-box","sing-box-vpngate") if subprocess.run(["systemctl","is-active","--quiet",name],capture_output=True).returncode==0]
    def restart(self,name):
        subprocess.run(["systemctl","restart",name],capture_output=True,check=True,timeout=45)
        time.sleep(.5)
        subprocess.run(["systemctl","is-active","--quiet",name],capture_output=True,check=True,timeout=10)

def activate(root,candidate,restart=True,services=None):
    with core_lock(root):return _activate(root,candidate,restart,services)

def _activate(root,candidate,restart=True,services=None):
    root=Path(root);current=root/"sing-box";previous=root/"sing-box.previous"
    version=check_core(candidate)
    config_checks(root,candidate)
    service_manager=services or Services()
    active=service_manager.active() if restart else []
    with tempfile.TemporaryDirectory(prefix=".core-activate-",dir=root) as td:
        stage=Path(td)/"next";shutil.copyfile(candidate,stage);stage.chmod(0o755)
        had_current=current.exists()
        if had_current:
            backup=Path(td)/"previous";shutil.copyfile(current,backup);backup.chmod(0o755);os.replace(backup,previous)
        os.replace(stage,current)
        try:
            for name in active:service_manager.restart(name)
        except Exception as error:
            if had_current:
                restore=Path(td)/"restore";shutil.copyfile(previous,restore);restore.chmod(0o755);os.replace(restore,current)
                for name in active:
                    try:service_manager.restart(name)
                    except Exception:pass
            else:current.unlink(missing_ok=True)
            raise ValueError("新核心服务启动失败，已恢复原核心") from error
    return version

def update(root,version=None,restart=True):
    with core_lock(root):return _update(root,version,restart)

def _update(root,version=None,restart=True):
    root=Path(root);asset=release_asset(version)
    current=root/"sing-box"
    if current.exists() and check_core(current)==asset["version"]:
        config_checks(root,current);return {"version":asset["version"],"changed":False}
    with tempfile.TemporaryDirectory(prefix=".core-update-",dir=root) as td:
        candidate=Path(td)/"sing-box";download(asset,candidate)
        installed=_activate(root,candidate,restart)
    return {"version":installed,"changed":True}

def main():
    parser=argparse.ArgumentParser();parser.add_argument("--root",default="/etc/s-box")
    sub=parser.add_subparsers(dest="command",required=True)
    sub.add_parser("check")
    prepare=sub.add_parser("prepare");prepare.add_argument("--version");prepare.add_argument("--output",required=True)
    update_parser=sub.add_parser("update");update_parser.add_argument("--version");update_parser.add_argument("--no-restart",action="store_true")
    activate_parser=sub.add_parser("activate");activate_parser.add_argument("candidate");activate_parser.add_argument("--no-restart",action="store_true")
    sub.add_parser("rollback")
    args=parser.parse_args();root=Path(args.root)
    if args.command=="check":
        current=check_core(root/"sing-box") if (root/"sing-box").exists() else "未安装"
        latest=release_asset()["version"];print(json.dumps({"current":current,"latest_stable":latest,"update_available":current!=latest},ensure_ascii=False));return
    if sys.platform!="linux" or os.geteuid()!=0:raise ValueError("核心安装/更新要求 Linux root")
    root.mkdir(parents=True,exist_ok=True)
    if args.command=="prepare":
        if Path(args.output).resolve()==(root/"sing-box").resolve():raise ValueError("准备文件不能直接覆盖当前核心")
        print(download(release_asset(args.version),args.output))
    elif args.command=="activate":print(activate(root,args.candidate,not args.no_restart))
    elif args.command=="update":print(json.dumps(update(root,args.version,not args.no_restart),ensure_ascii=False))
    else:
        previous=root/"sing-box.previous"
        if not previous.exists():raise ValueError("没有可回滚的上一核心")
        print(activate(root,previous))

if __name__=="__main__":
    try:main()
    except (ValueError,OSError,KeyError,StopIteration,subprocess.SubprocessError,tarfile.TarError) as error:
        print("核心操作失败："+str(error),file=sys.stderr);sys.exit(1)
