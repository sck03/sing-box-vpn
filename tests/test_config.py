import base64
import importlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

PROJECT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT / "lib"))
import config
import render
import store

CORE = os.environ.get("SING_BOX_BINARY")

@unittest.skipUnless(CORE, "set SING_BOX_BINARY to the official 1.14.4 binary")
class ConfigurationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="sb-vpn-test-")
        self.root = Path(self.tmp.name)
        profile = config.new_profile(self.root, Path(CORE), set())
        self.app = {"version": "1.14.4", "name": "test", "address": "127.0.0.1", "profile": profile, "inbounds": render.make_inbounds(profile), "routing": {}, "warp": None}
        config.commit_app(self.root, Path(CORE), self.app)

    def tearDown(self):
        self.tmp.cleanup()

    def test_current_core_and_all_five_nodes(self):
        store.check_core(Path(CORE))
        client = store.read_json(self.root / "sbox.json")
        self.assertEqual(set(store.PROTOCOLS), {n["type"] for n in client["outbounds"] if n["type"] in store.PROTOCOLS})
        self.assertNotIn("geosite", client["route"])
        self.assertEqual("direct", store.read_json(self.root / "sb.json")["route"]["final"])

    def test_country_links_are_stable_and_never_in_auto(self):
        config.prepare_vpngate(self.root, Path(CORE), self.app, "JP")
        first = config.exports(self.root, Path(CORE), self.app)
        config.prepare_vpngate(self.root, Path(CORE), self.app, "JP")
        self.assertEqual(first, config.exports(self.root, Path(CORE), self.app))
        client = store.read_json(self.root / "sbox.json")
        auto = next(n for n in client["outbounds"] if n["tag"] == "auto")
        self.assertTrue(all("VPNGate" not in tag for tag in auto["outbounds"]))
        group = next(n for n in client["outbounds"] if n["tag"] == "vpngate")
        self.assertEqual(5, len(group["outbounds"]))
        self.assertTrue(all("VPNGate-JP" in tag for tag in group["outbounds"]))
        cfg = store.read_json(self.root / "vpngate" / "server.json")
        self.assertEqual({"socks"}, {n["type"] for n in cfg["outbounds"]})
        self.assertEqual({"action": "reject"}, cfg["route"]["rules"][-1])

    def test_country_switch_keeps_originals_and_separates_auth(self):
        original = json.dumps(self.app["inbounds"], sort_keys=True)
        config.prepare_vpngate(self.root, Path(CORE), self.app, "JP")
        old_links = config.exports(self.root, Path(CORE), self.app)
        config.prepare_vpngate(self.root, Path(CORE), self.app, "KR")
        links = config.exports(self.root, Path(CORE), self.app)
        self.assertEqual(original, json.dumps(self.app["inbounds"], sort_keys=True))
        self.assertTrue(set(old_links).issubset(set(links)))
        credentials = store.read_json(self.root / "vpngate" / "secrets.json")
        self.assertNotEqual(credentials["JP"]["socks_pass"], credentials["KR"]["socks_pass"])
        active = store.read_json(self.root / "vpngate" / "gateway.json")
        self.assertEqual("KR", active["country"])
        self.assertEqual(credentials["KR"]["socks_user"], active["proxy_user"])

    def test_invalid_config_does_not_replace_working_file(self):
        before = (self.root / "sb.json").read_bytes()
        with self.assertRaises(ValueError):
            store.checked_json(self.root / "sb.json", {"outbounds": [{"type": "not-a-real-outbound"}]}, Path(CORE))
        self.assertEqual(before, (self.root / "sb.json").read_bytes())

    def test_recover_old_reality_public_key(self):
        pair = store.generate_reality(Path(CORE))
        self.assertEqual(pair["public_key"], store.derive_public(pair["private_key"]))

    def test_import_preserves_existing_credentials_and_ports(self):
        original = store.read_json(self.root / "sb.json")
        original["inbounds"][0]["sniff"] = True
        store.atomic_json(self.root / "sb.json", original)
        profile, imported, _, _ = config.import_existing(self.root, Path(CORE), set())
        for old, new in zip(original["inbounds"], imported):
            self.assertEqual(old["listen_port"], new["listen_port"])
            self.assertEqual(old["users"], new["users"])
        self.assertNotIn("sniff", imported[0])
        store.checked_json(self.root / "imported.json", render.server_config(imported), Path(CORE))

    def test_hopping_is_valid_and_does_not_enter_auto_as_vpngate(self):
        self.app["profile"]["hopping"] = ["31000:31100", "31200"]
        config.exports(self.root, Path(CORE), self.app)
        client = store.read_json(self.root / "sbox.json")
        self.assertEqual(["31000:31100", "31200:31200"], next(n for n in client["outbounds"] if n["type"] == "hysteria2")["server_ports"])

    def test_mihomo_yaml_json_subset_has_manual_only_gateway_group(self):
        config.prepare_vpngate(self.root, Path(CORE), self.app, "JP")
        config.exports(self.root, Path(CORE), self.app)
        cfg = store.read_json(self.root / "clmi.yaml")
        group = next(g for g in cfg["proxy-groups"] if g["name"] == "临时家宽")
        self.assertEqual("select", group["type"])
        self.assertNotIn("DIRECT", group["proxies"])

if __name__ == "__main__":
    unittest.main()
