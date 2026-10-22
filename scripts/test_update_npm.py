"""Isolated updater tests: real temporary archives, mocked Docker only."""

import importlib.util
import io
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("update_npm", Path(__file__).with_name("update_npm.py"))
updater = importlib.util.module_from_spec(spec)
spec.loader.exec_module(updater)


class UpdateNPMTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.work = Path(self.temporary.name)
        self.compose = self.work / "docker-compose.yml"
        self.original = 'services:\n  app:\n    image: "jc21/nginx-proxy-manager:2.13.5" # keep comment\n    network_mode: host\n'
        self.compose.write_text(self.original)
        self.data = self.work / "npm-data"
        self.ssl = self.work / "npm-ssl"
        for directory in (self.data, self.ssl):
            directory.mkdir()
            (directory / "state").write_text("original")
        self.calls = []
        self.old_image = "jc21/nginx-proxy-manager:2.13.5"
        self.container = {"Config": {"Image": self.old_image, "Labels": {
            "com.docker.compose.project.working_dir": str(self.work),
            "com.docker.compose.project.config_files": str(self.compose),
            "com.docker.compose.service": "app",
        }}, "Image": "sha256:old", "Mounts": [
            {"Destination": "/data", "Type": "bind", "Source": str(self.data)},
            {"Destination": "/etc/letsencrypt", "Type": "bind", "Source": str(self.ssl)},
        ]}
        self.manifest = {"target": updater.target_image(), "target_id": "sha256:new", "templates": {
            "proxy.conf": {"path": "/etc/nginx/conf.d/include/proxy.conf", "original": "new native", "patched": "new adapter"}
        }}
        self.real_run = updater.run
        self.addCleanup(patch.stopall)
        patch.object(updater, "run", side_effect=self.mock_run).start()
        patch.object(updater, "adapter_manifest", return_value=self.manifest).start()
        patch.object(updater, "wait_ready").start()
        patch("sys.stdout", new=io.StringIO()).start()

    def mock_run(self, *args, capture=False):
        self.calls.append(args)
        if args[0] == "tar":
            return self.real_run(*args, capture=capture)
        if args[:2] == ("docker", "inspect"):
            return json.dumps([self.container])
        if args[:3] == ("docker", "image", "inspect"):
            return "sha256:new" if args[-1] == updater.target_image() else "sha256:old"
        if args[:3] == ("docker", "exec", "npm"):
            return "old adapter"
        if args[-3:] == ("config", "--format", "json"):
            return json.dumps({"services": {"app": {"image": self.old_image}}})
        return ""

    def test_update_and_rollback_preserve_configuration_and_data(self):
        updater.update()
        self.assertEqual(self.compose.read_text(), self.original.replace('"' + self.old_image + '"', updater.target_image()))
        backup = next((self.work / "npm-update-backups").iterdir())
        self.assertEqual(backup.stat().st_mode & 0o777, 0o700)
        commands = [list(call) for call in self.calls]
        stop = next(i for i, call in enumerate(commands) if "stop" in call)
        archive = next(i for i, call in enumerate(commands) if call[0] == "tar")
        start = next(i for i, call in enumerate(commands) if "up" in call)
        self.assertLess(stop, archive)
        self.assertLess(archive, start)
        for directory in (self.data, self.ssl):
            (directory / "state").write_text("migrated")
        updater.rollback(backup)
        self.assertEqual(self.compose.read_text(), self.original)
        for directory in (self.data, self.ssl):
            self.assertEqual((directory / "state").read_text(), "original")
            preserved = next(self.work.glob(directory.name + ".before-rollback-*"))
            self.assertEqual((preserved / "state").read_text(), "migrated")

    def test_saved_pool_requires_explicit_reset(self):
        with sqlite3.connect(self.work / "data.db") as connection:
            self.addCleanup(connection.close)
            connection.execute("CREATE TABLE npm_upstreams (id INTEGER)")
            connection.execute("INSERT INTO npm_upstreams VALUES (1)")
        with self.assertRaisesRegex(RuntimeError, "--reset-upstreams"):
            updater.update()
        self.assertFalse(any("pull" in call or "stop" in call for call in self.calls))

    def test_reset_removes_pooled_resources_and_rollback_restores_all_state(self):
        with sqlite3.connect(self.work / "data.db") as connection:
            self.addCleanup(connection.close)
            connection.executescript("CREATE TABLE npm_upstreams (id INTEGER); INSERT INTO npm_upstreams VALUES (1); CREATE TABLE npm_upstream_bindings(consumer_type TEXT,npm_id INTEGER); INSERT INTO npm_upstream_bindings VALUES('proxy',3); CREATE TABLE npm_upstream_operations(state TEXT); INSERT INTO npm_upstream_operations VALUES('prepared');")
        with sqlite3.connect(self.data / "database.sqlite") as connection:
            self.addCleanup(connection.close)
            connection.executescript("CREATE TABLE proxy_host(id INTEGER,forward_host TEXT,locations TEXT,is_deleted INTEGER,enabled INTEGER); CREATE TABLE stream(id INTEGER,forwarding_host TEXT,is_deleted INTEGER,enabled INTEGER); INSERT INTO proxy_host VALUES (1,'hh_upstream_1','[]',0,1),(2,'direct.test','[]',0,1),(3,'direct.test','[]',0,1),(4,'direct.test','[{\"forward_host\":\"hh_upstream_1\"}]',0,1); INSERT INTO stream VALUES(1,'hh_upstream_2',0,1),(2,'direct.test',0,1);")
        assets = self.data / "hyperhive-upstream-adapter"
        assets.mkdir()
        (assets / "proxy.conf").write_text("old adapter")
        custom = self.data / "nginx/custom"
        custom.mkdir(parents=True)
        (custom / "http_top.conf").write_text("# keep this user hook\n# HyperHive managed upstream pools\ninclude /data/nginx/custom/hyperhive/current/http.conf;\n")
        (custom / "hyperhive").mkdir()
        generated = self.data / "nginx/proxy_host"
        generated.mkdir()
        (generated / "1.conf").write_text("pooled")
        (generated / "2.conf").write_text("direct")
        (self.work / "512SvMan.npm-previous").write_bytes(b"old master")
        (self.work / "512SvMan").write_bytes(b"new master")
        override = self.work / "docker-compose.hyperhive-upstreams.yml"
        original_override = "services:\n  app:\n    image: " + self.old_image + "\n"
        override.write_text(original_override)
        self.container["Config"]["Labels"]["com.docker.compose.project.config_files"] += "," + str(override)
        updater.update(reset=True)
        self.assertFalse(assets.exists())
        self.assertFalse(override.exists())
        self.assertIn("# keep this user hook", (custom / "http_top.conf").read_text())
        self.assertNotIn("include /data/nginx/custom/hyperhive", (custom / "http_top.conf").read_text())
        self.assertFalse((generated / "1.conf").exists())
        self.assertEqual((generated / "2.conf").read_text(), "direct")
        with sqlite3.connect(self.data / "database.sqlite") as connection:
            self.addCleanup(connection.close)
            self.assertEqual(connection.execute("SELECT id FROM proxy_host WHERE is_deleted=0").fetchall(), [(2,)])
            self.assertEqual(connection.execute("SELECT id FROM stream WHERE is_deleted=0").fetchall(), [(2,)])
        backup = next((self.work / "npm-update-backups").iterdir())
        updater.rollback(backup)
        self.assertEqual(override.read_text(), original_override)
        self.assertEqual((assets / "proxy.conf").read_text(), "old adapter")
        self.assertEqual((self.work / "512SvMan").read_bytes(), b"old master")
        with sqlite3.connect(self.work / "data.db") as connection:
            self.addCleanup(connection.close)
            self.assertEqual(connection.execute("SELECT id FROM npm_upstreams").fetchall(), [(1,)])

    def test_pending_operation_blocks(self):
        with sqlite3.connect(self.work / "data.db") as connection:
            self.addCleanup(connection.close)
            connection.execute("CREATE TABLE npm_upstream_operations (state TEXT)")
            connection.execute("INSERT INTO npm_upstream_operations VALUES ('prepared')")
        with self.assertRaisesRegex(RuntimeError, "Unfinished upstream"):
            updater.update()
        self.assertFalse(any("pull" in call or "stop" in call for call in self.calls))

    def test_backup_failure_restarts_original_container(self):
        original_run = self.mock_run

        def fail_archive(*args, **kwargs):
            if args[0] == "tar":
                raise OSError("disk full")
            return original_run(*args, **kwargs)

        with patch.object(updater, "run", side_effect=fail_archive):
            with self.assertRaisesRegex(OSError, "disk full"):
                updater.update()
        self.assertEqual(self.compose.read_text(), self.original)
        self.assertTrue(any("start" in call for call in self.calls))
        self.assertFalse(any("up" in call for call in self.calls))

    def test_ambiguous_compose_is_rejected(self):
        with self.assertRaisesRegex(RuntimeError, "exactly one"):
            updater.replace_image(self.original + "  other:\n    image: unrelated:1\n", updater.target_image())

    def test_target_follows_go_constant(self):
        with patch.object(Path, "read_text", return_value='const (\n image = "jc21/nginx-proxy-manager:9.8.7"\n)'):
            self.assertEqual(updater.target_image(), "jc21/nginx-proxy-manager:9.8.7")
        with patch.object(Path, "read_text", return_value="const unrelated = 1"):
            with self.assertRaisesRegex(RuntimeError, "one NPM image constant"):
                updater.target_image()


if __name__ == "__main__":
    unittest.main()
