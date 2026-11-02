#!/usr/bin/env python3
"""Explicitly invoked Telegram and private GitLab publishing."""
import argparse
import getpass
import json
import sys
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen
from store import atomic_json, read_json

def request(url, method="GET", data=None, headers=None):
    body=json.dumps(data).encode() if data is not None else None
    req=Request(url,data=body,method=method,headers={"Content-Type":"application/json",**(headers or {})})
    with urlopen(req,timeout=20) as response:return json.loads(response.read(2<<20))

def main():
    p=argparse.ArgumentParser();p.add_argument("--root",default="/etc/s-box");p.add_argument("command",choices=("telegram-config","telegram-push","gitlab-config","gitlab-push"));a=p.parse_args();root=Path(a.root);path=root/"remote.json";settings=read_json(path,{})
    if a.command=="telegram-config":
        settings["telegram"]={"token":getpass.getpass("Bot token（隐藏）："),"chat_id":input("你自己的 chat_id：").strip()};atomic_json(path,settings);return
    if a.command=="gitlab-config":
        settings["gitlab"]={"project":input("GitLab 私有项目 ID 或 group/project：").strip(),"branch":input("分支（默认 main）：").strip() or "main","token":getpass.getpass("访问令牌（隐藏）：")};atomic_json(path,settings);return
    if a.command=="telegram-push":
        cfg=settings.get("telegram");
        if not cfg:raise ValueError("先配置 Telegram")
        text=(root/"jhsub.txt").read_text();chunks=[];chunk=""
        for line in text.splitlines():
            if len(chunk)+len(line)>3500:chunks.append(chunk);chunk=""
            chunk+=line+"\n"
        if chunk:chunks.append(chunk)
        for chunk in chunks:request("https://api.telegram.org/bot"+cfg["token"]+"/sendMessage","POST",{"chat_id":cfg["chat_id"],"text":chunk,"disable_web_page_preview":True})
        print("节点已发送到配置的 Telegram 会话");return
    cfg=settings.get("gitlab")
    if not cfg:raise ValueError("先配置 GitLab")
    project=quote(cfg["project"],safe="");base="https://gitlab.com/api/v4/projects/"+project;headers={"PRIVATE-TOKEN":cfg["token"]}
    meta=request(base,headers=headers)
    if meta.get("visibility")!="private":raise ValueError("节点订阅只允许写入私有 GitLab 项目")
    for name in ("sbox.json","clmi.yaml","jhsub.txt"):
        url=base+"/repository/files/"+quote(name,safe="");method="PUT"
        try:request(url+"?ref="+quote(cfg["branch"]),headers=headers)
        except HTTPError as error:
            if error.code==404:method="POST"
            else:raise
        request(url,method,{"branch":cfg["branch"],"content":(root/name).read_text(),"commit_message":"更新 sing-box-vpn 订阅"},headers)
    print("私有订阅已更新；在 GitLab 仓库中查看三个订阅文件")

if __name__=="__main__":
    try:main()
    except (HTTPError,URLError) as error:print("远端请求失败，检查配置与网络；凭据未输出",file=sys.stderr);sys.exit(1)
    except (ValueError,OSError,KeyError) as error:print(str(error),file=sys.stderr);sys.exit(1)
