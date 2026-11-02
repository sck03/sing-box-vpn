#!/usr/bin/env python3
"""Token-protected subscription server, without directory listing or HTTP logs."""
import argparse
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit
from store import read_json

def serve(root):
    root=Path(root);settings=read_json(root/"subscription.json")
    if not settings or not settings.get("enabled") or len(settings.get("token",""))<32:
        raise ValueError("订阅服务未配置强路径令牌")
    files={"sbox.json":("sbox.json","application/json"),"clmi.yaml":("clmi.yaml","application/yaml"),"jhsub.txt":("jhsub-base64.txt","text/plain"),"links.txt":("jhsub.txt","text/plain")}
    class Handler(BaseHTTPRequestHandler):
        def log_message(self,*args):pass
        def do_GET(self):
            parts=urlsplit(self.path).path.strip("/").split("/")
            if len(parts)!=2 or not hmac.compare_digest(parts[0],settings["token"]) or parts[1] not in files:
                self.send_error(404);return
            filename,kind=files[parts[1]]
            try:data=(root/filename).read_bytes()
            except FileNotFoundError:self.send_error(503);return
            self.send_response(200);self.send_header("Content-Type",kind+"; charset=utf-8");self.send_header("Content-Length",str(len(data)));self.send_header("Cache-Control","no-store");self.end_headers();self.wfile.write(data)
    server=ThreadingHTTPServer((settings.get("listen","0.0.0.0"),int(settings["port"])),Handler)
    server.daemon_threads=True;server.serve_forever()

if __name__=="__main__":
    parser=argparse.ArgumentParser();parser.add_argument("--root",default="/etc/s-box");serve(parser.parse_args().root)
