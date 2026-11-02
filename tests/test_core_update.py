import hashlib
import io
import json
import os
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/"lib"))
import core
import store

def release(version="1.15.0",digest="a"*64):
    name=f"sing-box-{version}-linux-amd64.tar.gz"
    return {"tag_name":"v"+version,"draft":False,"prerelease":False,"assets":[{"name":name,"browser_download_url":f"https://github.com/SagerNet/sing-box/releases/download/v{version}/{name}","digest":"sha256:"+digest}]}

class FakeServices:
    def __init__(self,root,fail=False):self.root=root;self.fail=fail;self.calls=[]
    def active(self):return ["sing-box"]
    def restart(self,name):
        self.calls.append(name)
        if self.fail and (self.root/"sing-box").read_bytes()==b"new":raise OSError("startup failed")

class CoreUpdateTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.root=Path(self.tmp.name)
        (self.root/"sing-box").write_bytes(b"old");self.candidate=self.root/"candidate";self.candidate.write_bytes(b"new")
        (self.root/"sb.json").write_text('{"credentials":"unchanged"}')
    def tearDown(self):self.tmp.cleanup()

    def test_future_stable_version_is_not_locked_to_baseline(self):
        self.assertEqual("1.15.0",core.select_asset(release(),"amd64")["version"])
        result=subprocess.CompletedProcess([],0,stdout="sing-box version 1.15.0\n")
        with mock.patch("store.subprocess.run",return_value=result):self.assertEqual("1.15.0",store.check_core("binary"))

    def test_old_and_prerelease_versions_are_rejected(self):
        for version in ("1.10.7","1.13.0","1.15.0-alpha.1"):
            with self.assertRaises(ValueError):core.stable_version(version)
        data=release();data["prerelease"]=True
        with self.assertRaises(ValueError):core.select_asset(data,"amd64")

    def test_download_requires_official_url_and_digest(self):
        data=release();data["assets"][0]["browser_download_url"]="https://other.example/core.tar.gz"
        with self.assertRaises(ValueError):core.select_asset(data,"amd64")
        data=release();data["assets"][0].pop("digest")
        with self.assertRaises(ValueError):core.select_asset(data,"amd64")

    def test_bad_hash_keeps_existing_candidate(self):
        asset=core.select_asset(release(),"amd64")
        with mock.patch("core.request",return_value=io.BytesIO(b"bad archive")):
            with self.assertRaises(ValueError):core.download(asset,self.candidate)
        self.assertEqual(b"new",self.candidate.read_bytes())

    def test_invalid_archive_link_is_not_extracted(self):
        payload=io.BytesIO()
        with tarfile.open(fileobj=payload,mode="w:gz") as tar:
            member=tarfile.TarInfo("sing-box-1.15.0-linux-amd64/sing-box");member.type=tarfile.SYMTYPE;member.linkname="/etc/shadow";tar.addfile(member)
        data=payload.getvalue();asset=core.select_asset(release(digest=hashlib.sha256(data).hexdigest()),"amd64")
        with mock.patch("core.request",return_value=io.BytesIO(data)),mock.patch("core.architecture",return_value="amd64"):
            with self.assertRaises(ValueError):core.download(asset,self.candidate)
        self.assertEqual(b"new",self.candidate.read_bytes())

    def test_incompatible_config_never_replaces_core(self):
        with mock.patch("core.check_core",return_value="1.15.0"),mock.patch("core.config_checks",side_effect=ValueError("incompatible")):
            with self.assertRaises(ValueError):core.activate(self.root,self.candidate,services=FakeServices(self.root))
        self.assertEqual(b"old",(self.root/"sing-box").read_bytes())

    def test_update_preserves_config_and_only_restarts_active_service(self):
        services=FakeServices(self.root);before=(self.root/"sb.json").read_bytes()
        with mock.patch("core.check_core",return_value="1.15.0"),mock.patch("core.config_checks"):
            core.activate(self.root,self.candidate,services=services)
        self.assertEqual(b"new",(self.root/"sing-box").read_bytes())
        self.assertEqual(b"old",(self.root/"sing-box.previous").read_bytes())
        self.assertEqual(before,(self.root/"sb.json").read_bytes());self.assertEqual(["sing-box"],services.calls)

    def test_startup_failure_restores_old_binary(self):
        services=FakeServices(self.root,fail=True)
        with mock.patch("core.check_core",return_value="1.15.0"),mock.patch("core.config_checks"):
            with self.assertRaises(ValueError):core.activate(self.root,self.candidate,services=services)
        self.assertEqual(b"old",(self.root/"sing-box").read_bytes());self.assertEqual(["sing-box","sing-box"],services.calls)

    def test_rollback_keeps_the_replaced_core_as_previous(self):
        previous=self.root/"sing-box.previous";previous.write_bytes(b"previous")
        with mock.patch("core.check_core",return_value="1.14.4"),mock.patch("core.config_checks"):
            core.activate(self.root,previous,restart=False)
        self.assertEqual(b"previous",(self.root/"sing-box").read_bytes())
        self.assertEqual(b"old",previous.read_bytes())

    def test_config_edit_during_validation_is_detected(self):
        def checked(*args,**kwargs):
            (self.root/"sb.json").write_text('{}');return subprocess.CompletedProcess([],0)
        with mock.patch("core.subprocess.run",side_effect=checked):
            with self.assertRaises(ValueError):core.config_checks(self.root,self.candidate)

    def test_two_updates_cannot_hold_lock(self):
        with core.core_lock(self.root):
            with self.assertRaises(ValueError):
                with core.core_lock(self.root):pass

if __name__=="__main__":unittest.main()
