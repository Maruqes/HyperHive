"""Opt-in upgrade/reset/rollback test using disposable Docker bind mounts only.

Run as root with HYPERHIVE_NPM_TEST_MASTER pointing to a freshly built master,
and HYPERHIVE_NPM_TEST_ROOT pointing to an empty host-visible scratch directory.
No production container, credentials or ports are used.
"""

from contextlib import closing
import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import uuid

spec = importlib.util.spec_from_file_location("update_npm", Path(__file__).with_name("update_npm.py"))
updater = importlib.util.module_from_spec(spec)
spec.loader.exec_module(updater)


@unittest.skipUnless(os.environ.get("HYPERHIVE_NPM_TEST_MASTER"), "requires disposable Docker fixture")
class DockerUpgradeTest(unittest.TestCase):
    def test_reset_upgrade_and_rollback(self):
        self.assertEqual(os.geteuid(), 0, "archive fidelity test requires root")
        manifest = json.loads(subprocess.check_output([os.environ["HYPERHIVE_NPM_TEST_MASTER"], "--npm-upgrade-manifest"], text=True))
        name = "hh-npm-update-test-" + uuid.uuid4().hex[:12]
        real_run = updater.run

        def isolated_run(*args, **kwargs):
            if args[:2] in (("docker", "inspect"), ("docker", "exec")) and args[2] == "npm":
                args = (*args[:2], name, *args[3:])
            return real_run(*args, **kwargs)

        with tempfile.TemporaryDirectory(dir=os.environ["HYPERHIVE_NPM_TEST_ROOT"]) as scratch:
            work = Path(scratch)
            data, ssl = work / "npm-data", work / "npm-ssl"
            data.mkdir()
            ssl.mkdir()
            path = work / "docker-compose.yml"
            fixture = str(Path(__file__).resolve().parents[1] / "master/internal/testnpm")
            config = {"services": {"app": {"image": "jc21/nginx-proxy-manager:2.13.5", "container_name": name, "network_mode": "none",
                "environment": {"INITIAL_ADMIN_EMAIL": "fixture@example.test", "INITIAL_ADMIN_PASSWORD": "Disposable-fixture-Only123!"},
                "volumes": [str(data) + ":/data", str(ssl) + ":/etc/letsencrypt", fixture + ":/fixture:ro"]}}}
            # Keep the generated project's one-image YAML layout, as required by the updater.
            path.write_text("services:\n  app:\n    image: jc21/nginx-proxy-manager:2.13.5\n" + "\n".join(
                "    " + key + ": " + json.dumps(value) for key, value in config["services"]["app"].items() if key != "image") + "\n")
            original = path.read_text()
            backup = None
            try:
                with patch.object(updater, "run", side_effect=isolated_run), patch.object(updater, "adapter_manifest", return_value=manifest):
                    updater.compose(path, "up", "-d")
                    updater.wait_ready()
                    real_run("docker", "exec", "-d", name, "node", "/fixture/backends.cjs")

                    def api(method, endpoint, body=None):
                        code = "(async()=>{const token=(await (await fetch('http://127.0.0.1:81/api/tokens',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({identity:'fixture@example.test',secret:'Disposable-fixture-Only123!'})})).json()).token;const r=await fetch('http://127.0.0.1:81/api' + " + json.dumps(endpoint) + ",{method:" + json.dumps(method) + ",headers:{'Content-Type':'application/json',Authorization:'Bearer '+token},body:" + (json.dumps(json.dumps(body)) if body is not None else "undefined") + "});if(!r.ok)throw Error('API '+r.status);console.log(await r.text())})().catch(e=>{console.error(e);process.exit(1)})"
                        return json.loads(real_run("docker", "exec", name, "node", "-e", code, capture=True))

                    direct = api("POST", "/nginx/proxy-hosts", {"domain_names": ["direct.test"], "forward_scheme": "http", "forward_host": "127.0.0.1", "forward_port": 18080, "locations": [], "access_list_id": 0})
                    pooled = api("POST", "/nginx/proxy-hosts", {"domain_names": ["pool.test"], "forward_scheme": "http", "forward_host": "hh_upstream_1", "forward_port": 80, "locations": [], "access_list_id": 0})
                    stream = api("POST", "/nginx/streams", {"incoming_port": 16060, "forwarding_host": "hh_upstream_2", "forwarding_port": 80, "tcp_forwarding": True, "udp_forwarding": True})
                    direct_stream = api("POST", "/nginx/streams", {"incoming_port": 16061, "forwarding_host": "127.0.0.1", "forwarding_port": 19090, "tcp_forwarding": True, "udp_forwarding": False})
                    custom = api("POST", "/nginx/proxy-hosts", {"domain_names": ["custom.test"], "forward_scheme": "http", "forward_host": "127.0.0.1", "forward_port": 18080, "locations": [{"path": "/pooled", "forward_scheme": "http", "forward_host": "hh_upstream_1", "forward_port": 80}], "access_list_id": 0})
                    with closing(sqlite3.connect(work / "data.db")) as connection, connection:
                        connection.executescript("CREATE TABLE npm_upstreams(id INTEGER PRIMARY KEY); INSERT INTO npm_upstreams VALUES(1); CREATE TABLE npm_upstream_bindings(consumer_type TEXT,npm_id INTEGER); CREATE TABLE npm_upstream_operations(state TEXT);")
                        connection.execute("INSERT INTO npm_upstream_bindings VALUES ('proxy',?)", (pooled["id"],))
                    adapter = data / "hyperhive-upstream-adapter"
                    adapter.mkdir()
                    (adapter / "marker").write_text("old adapter")
                    custom_hooks = data / "nginx/custom"
                    generation = custom_hooks / "hyperhive/generations/1"
                    generation.mkdir(parents=True)
                    (generation / "http.conf").write_text("upstream hh_upstream_1 { server 127.0.0.1:18080; }\n")
                    (generation / "stream.conf").write_text("upstream hh_upstream_2 { server 127.0.0.1:19090; }\n")
                    (custom_hooks / "hyperhive/current").symlink_to("generations/1")
                    for hook, context in (("http_top", "http"), ("stream", "stream")):
                        (custom_hooks / (hook + ".conf")).write_text("# preserved user hook\n# HyperHive managed upstream pools\ninclude /data/nginx/custom/hyperhive/current/" + context + ".conf;\n")
                    previous = work / "512SvMan.npm-previous"
                    previous.write_bytes(b"original master fixture")
                    previous.chmod(0o755)
                    (work / "512SvMan").write_bytes(b"new master fixture")
                    override = work / "docker-compose.hyperhive-upstreams.yml"
                    original_override = "services:\n  app:\n    image: jc21/nginx-proxy-manager:2.13.5\n"
                    override.write_text(original_override)
                    updater.update(reset=True)
                    backup = next((work / "npm-update-backups").iterdir())
                    self.assertEqual(real_run("docker", "inspect", name, "--format", "{{.Image}}", capture=True).strip(), manifest["target_id"])
                    self.assertFalse(adapter.exists())
                    self.assertFalse(override.exists())
                    self.assertEqual([row["id"] for row in api("GET", "/nginx/proxy-hosts")], [direct["id"]])
                    self.assertEqual([row["id"] for row in api("GET", "/nginx/streams")], [direct_stream["id"]])
                    with closing(sqlite3.connect(work / "data.db")) as connection:
                        self.assertEqual(connection.execute("SELECT count(*) FROM npm_upstreams").fetchone()[0], 0)
                    real_run("docker", "exec", "-d", name, "node", "/fixture/backends.cjs")
                    traffic = real_run("docker", "exec", name, "node", "-e", "require('node:http').get({host:'127.0.0.1',headers:{Host:'direct.test'}},r=>{let b='';r.on('data',v=>b+=v);r.on('end',()=>console.log(b))}).on('error',()=>process.exit(1))", capture=True).strip()
                    self.assertIn(traffic, ("member-1", "member-2"))
                    traffic = real_run("docker", "exec", name, "node", "-e", "const s=require('node:net').connect(16061,'127.0.0.1');s.on('data',b=>console.log(b.toString()));s.setTimeout(2000,()=>process.exit(1));", capture=True).strip()
                    self.assertIn(traffic, ("member-1", "member-2"))
                    updater.rollback(backup)
                    self.assertEqual(path.read_text(), original)
                    self.assertEqual(override.read_text(), original_override)
                    self.assertEqual((adapter / "marker").read_text(), "old adapter")
                    self.assertEqual((work / "512SvMan").read_bytes(), previous.read_bytes())
                    self.assertEqual({row["id"] for row in api("GET", "/nginx/proxy-hosts")}, {direct["id"], pooled["id"], custom["id"]})
                    self.assertEqual({row["id"] for row in api("GET", "/nginx/streams")}, {stream["id"], direct_stream["id"]})
            finally:
                subprocess.run(["docker", "rm", "-fv", name], check=False)
                if backup and (backup / "state.json").exists():
                    state = json.loads((backup / "state.json").read_text())
                    subprocess.run(["docker", "image", "rm", state["rollback_image"]], check=False)


if __name__ == "__main__":
    unittest.main()
