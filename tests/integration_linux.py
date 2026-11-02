"""Privileged, offline OpenVPN/namespace acceptance test for Linux CI."""
import base64
import csv
import http.server
import io
import json
import os
import signal
import socket
import ssl
import struct
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path

PROJECT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(PROJECT/"lib"))
from store import atomic_json

GATEWAY=os.environ["GATEWAY_BINARY"]
SERVER_NS="ygtest-server"
HOST_VETH="ygtest-host"
PEER_VETH="ygtest-peer"
REMOTE="203.0.113.2"
TARGET="198.51.100.10"
EXIT="198.51.100.99"
HOST_EXIT="198.51.100.88"
STATE={"country":"JP"}
CHILDREN=[]

def run(*args,check=True):return subprocess.run(args,check=check,capture_output=True,text=True).stdout.strip()
def spawn(args,log,env=None):
    stream=open(log,"wb");p=subprocess.Popen(args,stdout=stream,stderr=stream,env=env);CHILDREN.append((p,stream));return p
def stop(p):
    if p.poll() is None:
        p.terminate()
        try:p.wait(timeout=10)
        except subprocess.TimeoutExpired:p.kill();p.wait()
def wait_for(fn,seconds=90):
    end=time.monotonic()+seconds;last=None
    while time.monotonic()<end:
        try:
            value=fn()
            if value:return value
        except Exception as error:last=error
        time.sleep(.2)
    raise TimeoutError(str(last or "condition not reached"))
def ctl(config,op,*args):return json.loads(run(GATEWAY,"ctl",op,"--config",str(config),*args))
def receive(sock,n):
    b=b""
    while len(b)<n:
        data=sock.recv(n-len(b))
        if not data:raise EOFError
        b+=data
    return b
def connect_proxy(port,command,target,domain=False):
    c=socket.create_connection(("127.0.0.1",port),timeout=3);c.settimeout(3);c.sendall(b"\x05\x01\x02");assert receive(c,2)==b"\x05\x02"
    user,password=b"ygvg-JP",b"a"*64;c.sendall(bytes([1,len(user)])+user+bytes([len(password)])+password);assert receive(c,2)==b"\x01\x00"
    if domain:addr=bytes([3,len(target[0])])+target[0].encode()
    else:addr=bytes([1])+socket.inet_aton(target[0])
    c.sendall(bytes([5,command,0])+addr+struct.pack("!H",target[1]));h=receive(c,4)
    if h[1]!=0:c.close();raise ConnectionError("proxy refused")
    assert h[3]==1;ip=socket.inet_ntoa(receive(c,4));bound=struct.unpack("!H",receive(c,2))[0];return c,(ip,bound)

def data_server(root):
    # Spawned under the test server namespace; no host routes are changed here.
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self,*args):pass
        def do_GET(self):
            value=EXIT if self.client_address[0].startswith("10.8.") else HOST_EXIT
            body=value.encode();self.send_response(200);self.send_header("Content-Length",str(len(body)));self.end_headers();self.wfile.write(body)
    http=http.server.ThreadingHTTPServer((TARGET,8443),Handler)
    context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.load_cert_chain(str(root/"server.crt"),str(root/"server.key"));http.socket=context.wrap_socket(http.socket,server_side=True)
    threading.Thread(target=http.serve_forever,daemon=True).start()
    tcp=socket.socket();tcp.bind((TARGET,9000));tcp.listen()
    def tcp_loop():
        while True:
            c,_=tcp.accept()
            def echo(c):
                try:
                    while True:
                        b=c.recv(65535)
                        if not b:break
                        c.sendall(b)
                finally:c.close()
            threading.Thread(target=echo,args=(c,),daemon=True).start()
    threading.Thread(target=tcp_loop,daemon=True).start()
    udp=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);udp.bind((TARGET,9001))
    def udp_loop():
        while True:b,a=udp.recvfrom(65535);udp.sendto(b,a)
    threading.Thread(target=udp_loop,daemon=True).start()
    dns=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);dns.bind(("1.1.1.1",53))
    while True:
        packet,addr=dns.recvfrom(4096);offset=12
        while packet[offset]:offset+=1+packet[offset]
        offset+=1;kind=struct.unpack("!H",packet[offset:offset+2])[0];question=packet[12:offset+4]
        header=packet[:2]+struct.pack("!HHHHH",0x8180,1,int(kind==1),0,0)
        answer=b"\xc0\x0c"+struct.pack("!HHIH",1,1,60,4)+socket.inet_aton(TARGET) if kind==1 else b""
        dns.sendto(header+question+answer,addr)
        with open(root/"dns-sources.log","a") as f:f.write(addr[0]+"\n")

def main():
    if os.geteuid()!=0 or sys.platform!="linux":raise RuntimeError("run with sudo on Linux")
    if "ygvg0" in run("ip","netns","list") or SERVER_NS in run("ip","netns","list"):raise RuntimeError("test namespaces already exist")
    hosts=Path("/etc/hosts").read_bytes();forward=run("sysctl","-n","net.ipv4.ip_forward")
    default_route=run("ip","-4","route","show","default")
    with tempfile.TemporaryDirectory(prefix="ygtest-",dir="/tmp") as td:
        root=Path(td);config=root/"gateway.json";daemon=None
        try:
            run("ip","netns","add",SERVER_NS);run("ip","link","add",HOST_VETH,"type","veth","peer","name",PEER_VETH);run("ip","link","set",PEER_VETH,"netns",SERVER_NS)
            run("ip","addr","add","203.0.113.1/30","dev",HOST_VETH);run("ip","link","set",HOST_VETH,"up")
            for args in (("link","set","lo","up"),("addr","add",REMOTE+"/30","dev",PEER_VETH),("link","set",PEER_VETH,"up"),("addr","add",TARGET+"/32","dev","lo"),("addr","add","1.1.1.1/32","dev","lo")):
                run("ip","netns","exec",SERVER_NS,"ip",*args)
            run("ip","route","add",TARGET+"/32","via",REMOTE,"dev",HOST_VETH)
            Path("/etc/hosts").write_bytes(hosts+b"\n"+TARGET.encode()+b" exit.test\n")
            run("openssl","req","-x509","-newkey","rsa:2048","-nodes","-days","1","-subj","/CN=yg-test-ca","-keyout",str(root/"ca.key"),"-out",str(root/"ca.crt"))
            for role in ("server","client"):
                run("openssl","req","-new","-newkey","rsa:2048","-nodes","-subj","/CN="+("exit.test" if role=="server" else "client"),"-keyout",str(root/(role+".key")),"-out",str(root/(role+".csr")))
                (root/(role+".ext")).write_text("extendedKeyUsage="+("serverAuth" if role=="server" else "clientAuth")+"\n"+("subjectAltName=DNS:exit.test\n" if role=="server" else ""))
                run("openssl","x509","-req","-in",str(root/(role+".csr")),"-CA",str(root/"ca.crt"),"-CAkey",str(root/"ca.key"),"-CAcreateserial","-days","1","-extfile",str(root/(role+".ext")),"-out",str(root/(role+".crt")))
            server_config="\n".join(["port 1194","proto udp","local "+REMOTE,"dev tun0","server 10.8.0.0 255.255.255.0","topology subnet","dh none","ca "+str(root/"ca.crt"),"cert "+str(root/"server.crt"),"key "+str(root/"server.key"),"tls-version-min 1.2","data-ciphers AES-256-GCM:AES-128-GCM","auth SHA256","push \"redirect-gateway def1\"","keepalive 5 20","verb 3"])+"\n"
            (root/"server.conf").write_text(server_config)
            spawn(["ip","netns","exec",SERVER_NS,"openvpn","--config",str(root/"server.conf")],root/"server.log")
            spawn(["ip","netns","exec",SERVER_NS,sys.executable,str(Path(__file__).resolve()),"--data-server",str(root)],root/"data.log")
            profile="client\ndev tun\nproto udp\nremote "+REMOTE+" 1194\nremote-cert-tls server\nauth SHA256\n"
            for kind,file in (("ca","ca.crt"),("cert","client.crt"),("key","client.key")):profile+="<"+kind+">\n"+(root/file).read_text()+"</"+kind+">\n"
            csv_buffer=io.StringIO();writer=csv.writer(csv_buffer);writer.writerow(["HostName","IP","CountryShort","Ping","Speed","OpenVPN_ConfigData_Base64"]);writer.writerow(["volunteer-test",REMOTE,"JP","1","10000",base64.b64encode(profile.encode()).decode()])
            class Control(http.server.BaseHTTPRequestHandler):
                def log_message(self,*args):pass
                def do_GET(self):
                    b=(csv_buffer.getvalue().encode() if self.path.startswith("/nodes") else json.dumps({"location":{"country_code":STATE["country"]},"asn":{"asn":42,"type":"isp","org":"test-isp"},"is_datacenter":False}).encode())
                    self.send_response(200);self.send_header("Content-Length",str(len(b)));self.end_headers();self.wfile.write(b)
            provider=http.server.ThreadingHTTPServer(("127.0.0.1",0),Control);threading.Thread(target=provider.serve_forever,daemon=True).start()
            listener=socket.socket();listener.bind(("127.0.0.1",0));port=listener.getsockname()[1];listener.close()
            cfg={"country":"JP","enabled":True,"listen":"127.0.0.1:"+str(port),"proxy_user":"ygvg-JP","proxy_pass":"a"*64,"data_dir":str(root/"data"),"run_dir":str(root/"run"),"sources":["http://127.0.0.1:"+str(provider.server_port)+"/nodes"],"geo_url":"http://127.0.0.1:"+str(provider.server_port)+"/geo?q=%s","probe_url":"https://exit.test:8443","health_interval":2,"connect_timeout":30}
            atomic_json(config,cfg);env=dict(os.environ,SSL_CERT_FILE=str(root/"ca.crt"))
            daemon=spawn([GATEWAY,"daemon","--config",str(config)],root/"daemon.log",env)
            wait_for(lambda:ctl(config,"status").get("state")=="ready")
            assert run("ip","-4","route","show","default")==default_route
            c,_=connect_proxy(port,1,(TARGET,9000));c.sendall(b"tcp-via-openvpn");assert receive(c,15)==b"tcp-via-openvpn"
            control,bound=connect_proxy(port,3,("0.0.0.0",0));udp=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);udp.settimeout(3)
            packet=bytes([0,0,0,1])+socket.inet_aton(TARGET)+struct.pack("!H",9001)+b"udp-via-openvpn";udp.sendto(packet,bound);data,_=udp.recvfrom(65535);assert data.endswith(b"udp-via-openvpn")
            by_name,_=connect_proxy(port,1,("business.test",9000),domain=True);by_name.sendall(b"dns-via-tun");assert receive(by_name,11)==b"dns-via-tun";by_name.close()
            dns_sources=(root/"dns-sources.log").read_text().splitlines();assert dns_sources and all(ip.startswith("10.8.") for ip in dns_sources),dns_sources
            print("OpenVPN handshake, TCP, UDP and tunnel DNS passed",flush=True)
            pids=run("ip","netns","pids","ygvg0").splitlines();pid=next(int(p) for p in pids if Path("/proc/"+p+"/comm").read_text().strip()=="openvpn")
            os.kill(pid,signal.SIGKILL)
            udp.settimeout(.5)
            try:udp.sendto(packet,bound);data,_=udp.recvfrom(65535);raise AssertionError("existing UDP session leaked after tunnel death")
            except (OSError,TimeoutError):pass
            # Both established and new sessions must fail after tunnel death.
            try:c.sendall(b"must-not-leak");assert c.recv(128)!=b"must-not-leak"
            except (OSError,EOFError):pass
            wait_for(lambda:ctl(config,"status").get("state")!="ready",10)
            try:connect_proxy(port,1,(TARGET,9000));raise AssertionError("new connection bypassed dead tunnel")
            except (OSError,EOFError,ConnectionError):pass
            c.close();control.close();udp.close();ctl(config,"stop");stop(daemon)
            assert read_enabled(config) is False
            daemon=spawn([GATEWAY,"daemon","--config",str(config)],root/"restart.log",env);wait_for(lambda:ctl(config,"status").get("state")=="stopped",10)
            assert "ygvg0" not in run("ip","netns","list")
            print("Tunnel death blocked existing/new sessions; disabled state survived restart",flush=True)
            STATE["country"]="KR";ctl(config,"start");wait_for(lambda:"does not match JP" in ctl(config,"status").get("reason",""))
            assert ctl(config,"status")["state"]!="ready";ctl(config,"stop");stop(daemon)
            print("Wrong-country exit rejected",flush=True)
            provider.shutdown()
        except Exception:
            for log in root.glob("*.log"):print(log.name+":\n"+log.read_text(errors="replace")[-12000:],file=sys.stderr)
            raise
        finally:
            for p,stream in CHILDREN:stop(p);stream.close()
            if config.exists():run(GATEWAY,"cleanup","--config",str(config),check=False)
            run("ip","netns","del",SERVER_NS,check=False);run("ip","link","del",HOST_VETH,check=False)
            Path("/etc/hosts").write_bytes(hosts);run("sysctl","-qw","net.ipv4.ip_forward="+forward,check=False)

def read_enabled(config):return json.loads(Path(config).read_text())["enabled"]
if __name__=="__main__":
    if len(sys.argv)>1 and sys.argv[1]=="--data-server":data_server(Path(sys.argv[2]))
    else:main()
