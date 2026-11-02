"""Exercise all five real sing-box protocols over loopback, including UDP.

The VPNGate side uses a controlled authenticated SOCKS5 peer here. Actual
OpenVPN/namespace leak protection is tested separately on Linux.
"""
import copy
import http.server
import json
import os
import socket
import socketserver
import ssl
import struct
import subprocess
import sys
import tempfile
import threading
import time
from contextlib import contextmanager
from pathlib import Path

PROJECT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(PROJECT/"lib"))
from config import new_profile
from render import make_inbounds,outbound,server_config
from store import atomic_json,available_port,checked_json

CORE=os.environ["SING_BOX_BINARY"]
PROCESSES=[]
@contextmanager
def test_directory():
    tmp=tempfile.TemporaryDirectory(prefix="sb-e2e-")
    try:yield Path(tmp.name)
    finally:
        for process,log in PROCESSES:
            if process.poll() is None:process.kill();process.wait()
            log.close()
        PROCESSES.clear()
        tmp.cleanup()
def receive(sock,n):
    value=b""
    while len(value)<n:
        chunk=sock.recv(n-len(value))
        if not chunk:raise EOFError("socket closed")
        value+=chunk
    return value
def address(sock,kind):
    if kind==1:host=socket.inet_ntoa(receive(sock,4))
    elif kind==3:host=receive(sock,receive(sock,1)[0]).decode()
    elif kind==4:host=socket.inet_ntop(socket.AF_INET6,receive(sock,16))
    else:raise ValueError("unknown address type")
    return host,struct.unpack("!H",receive(sock,2))[0]
def socks_header(host,port):return bytes([0,0,0,1])+socket.inet_aton(host)+struct.pack("!H",port)

class Echo(http.server.BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def do_GET(self):
        body=self.server.body
        self.send_response(200);self.send_header("Content-Length",str(len(body)));self.end_headers();self.wfile.write(body)
def http_echo(body):
    server=http.server.ThreadingHTTPServer(("127.0.0.1",0),Echo);server.body=body
    threading.Thread(target=server.serve_forever,daemon=True).start();return server

class Proxy(socketserver.BaseRequestHandler):
    def handle(self):
        c=self.request;c.settimeout(10)
        try:
            h=receive(c,2);methods=receive(c,h[1]);c.sendall(bytes([5,2]))
            if receive(c,1)!=b"\x01":return
            user=receive(c,receive(c,1)[0]).decode();password=receive(c,receive(c,1)[0]).decode()
            if user!=self.server.user or password!=self.server.password:c.sendall(b"\x01\x01");return
            c.sendall(b"\x01\x00");req=receive(c,4)
            if os.environ.get("E2E_DEBUG"):print("proxy raw",req.hex(),flush=True)
            target=address(c,req[3])
            if os.environ.get("E2E_DEBUG"):print("proxy request",req[1],req[3],target,flush=True)
            if req[1]==1:
                remote=socket.create_connection(self.server.echo,timeout=5)
                c.sendall(bytes([5,0,0,1,127,0,0,1,0,0]))
                def back():
                    try:
                        while True:
                            data=remote.recv(65535)
                            if not data:break
                            c.sendall(data)
                    except OSError:pass
                    finally:c.close();remote.close()
                threading.Thread(target=back,daemon=True).start()
                while True:
                    data=c.recv(65535)
                    if not data:break
                    remote.sendall(data)
            elif req[1]==3:
                relay=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);relay.bind(("127.0.0.1",0));relay.settimeout(10)
                c.sendall(bytes([5,0,0,1,127,0,0,1])+struct.pack("!H",relay.getsockname()[1]))
                try:
                    while True:
                        data,src=relay.recvfrom(65535)
                        if os.environ.get("E2E_DEBUG"):print("proxy UDP",len(data),data[:12].hex(),src,flush=True)
                        if data[2]!=0:continue
                        if data[3]==1:offset=10
                        elif data[3]==3:offset=7+data[4]
                        else:continue
                        relay.sendto(data[:offset]+b"vpn:"+data[offset:],src)
                finally:relay.close()
        except (OSError,EOFError,ValueError) as error:
            if os.environ.get("E2E_DEBUG"):print("proxy ended",repr(error),flush=True)

class ProxyServer(socketserver.ThreadingTCPServer):
    allow_reuse_address=True
    daemon_threads=True

def start_core(root,name,cfg,port):
    filename=root/(name+".json");checked_json(filename,cfg,CORE)
    log=open(root/(name+".log"),"wb")
    process=subprocess.Popen([CORE,"run","-c",str(filename)],stdout=log,stderr=log,cwd=root)
    PROCESSES.append((process,log))
    end=time.monotonic()+10
    while time.monotonic()<end:
        if process.poll() is not None:raise RuntimeError(name+" failed: "+(root/(name+".log")).read_text(errors="replace"))
        try:socket.create_connection(("127.0.0.1",port),timeout=.1).close();return process
        except OSError:time.sleep(.05)
    raise TimeoutError(name+" did not listen")

def mixed_connect(port,command,target):
    c=socket.create_connection(("127.0.0.1",port),timeout=15);c.settimeout(15);c.sendall(b"\x05\x01\x00")
    if receive(c,2)!=b"\x05\x00":raise AssertionError("mixed SOCKS greeting")
    c.sendall(bytes([5,command,0,1])+socket.inet_aton(target[0])+struct.pack("!H",target[1]));h=receive(c,4)
    if h[1]!=0:raise AssertionError("SOCKS request refused: "+str(h[1]))
    return c,address(c,h[3])

def main():
    with test_directory() as td:
        root=Path(td);used=set();profile=new_profile(root,CORE,used)
        tls_target=http_echo(b"tls-target");context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.minimum_version=ssl.TLSVersion.TLSv1_3
        context.load_cert_chain(profile["certificate"],profile["key"]);tls_target.socket=context.wrap_socket(tls_target.socket,server_side=True)
        profile.update(reality_sni="www.bing.com",handshake_host="127.0.0.1",handshake_port=tls_target.server_address[1])
        ordinary=http_echo(b"ordinary-exit");landing=http_echo(b"vpngate-exit")
        udp=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);udp.bind(("127.0.0.1",0))
        def udp_echo():
            try:
                while True:
                    b,a=udp.recvfrom(65535);udp.sendto(b"direct:"+b,a)
            except OSError:pass
        threading.Thread(target=udp_echo,daemon=True).start()
        ins=make_inbounds(profile);start_core(root,"ordinary",server_config(ins),ins[0]["listen_port"])
        proxy=ProxyServer(("127.0.0.1",0),Proxy);proxy.user="ygvg-JP";proxy.password="a"*64;proxy.echo=landing.server_address
        threading.Thread(target=proxy.serve_forever,daemon=True).start()
        vp=new_profile(root,CORE,used,"JP-");vp.update(reality_sni="www.bing.com",handshake_host="127.0.0.1",handshake_port=tls_target.server_address[1])
        vgins=make_inbounds(vp,"vg-JP-")
        gateway={"inbounds":vgins,"outbounds":[{"type":"socks","tag":"gateway","server":"127.0.0.1","server_port":proxy.server_address[1],"username":proxy.user,"password":proxy.password,"version":"5"}],"route":{"final":"gateway"}}
        start_core(root,"vpngate-entries",gateway,vgins[0]["listen_port"])
        for label,inputs,p in (("ordinary",ins,profile),("vpngate",vgins,vp)):
            for inbound in inputs:
                node=outbound(inbound,p,"127.0.0.1",label+"-"+inbound["type"])
                port=available_port(used)
                client={"inbounds":[{"type":"mixed","listen":"127.0.0.1","listen_port":port}],"outbounds":[node],"route":{"final":node["tag"]}}
                process=start_core(root,"client-"+node["tag"],client,port)
                try:
                    c,_=mixed_connect(port,1,ordinary.server_address);c.sendall(b"GET / HTTP/1.0\r\nHost: localhost\r\n\r\n");data=b""
                    while True:
                        b=c.recv(65535)
                        if not b:break
                        data+=b
                    c.close();expected=b"ordinary-exit" if label=="ordinary" else b"vpngate-exit"
                    if expected not in data:raise AssertionError(node["tag"]+" TCP reached wrong exit")
                    c,bound=mixed_connect(port,3,udp.getsockname());u=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);u.settimeout(15)
                    if os.environ.get("E2E_DEBUG"):print(node["tag"],"UDP bind",bound,flush=True)
                    u.sendto(socks_header(*udp.getsockname())+b"udp-check",bound);answer,_=u.recvfrom(65535);u.close();c.close()
                    expected=b"direct:udp-check" if label=="ordinary" else b"vpn:udp-check"
                    if not answer.endswith(expected):raise AssertionError(node["tag"]+" UDP reached wrong exit")
                    print(node["tag"]+": TCP and UDP passed",flush=True)
                finally:process.terminate();process.wait(timeout=10)
        udp.close();proxy.shutdown();proxy.server_close();ordinary.shutdown();landing.shutdown();tls_target.shutdown()

if __name__=="__main__":
    try:main()
    finally:
        for process,log in PROCESSES:
            if process.poll() is None:process.kill();process.wait()
            log.close()
