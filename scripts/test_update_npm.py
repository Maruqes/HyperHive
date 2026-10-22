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
        self.real_run = updater.run
        self.addCleanup(patch.stopall)
        patch.object(updater, "run", side_effect=self.mock_run).start()
        patch("sys.stdout", new=io.StringIO()).start()

    def mock_run(self, *args, capture=False):
        self.calls.append(args)
        if args[0] == "tar":
            return self.real_run(*args, capture=capture)
        if args[:2] == ("docker", "inspect"):
            return json.dumps([self.container])
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

    def test_saved_pool_blocks_before_pull_or_stop(self):
        with sqlite3.connect(self.work / "data.db") as connection:
            connection.execute("CREATE TABLE npm_upstreams (id INTEGER)")
            connection.execute("INSERT INTO npm_upstreams VALUES (1)")
        with self.assertRaisesRegex(RuntimeError, "Saved upstream pools"):
            updater.update()
        self.assertFalse(any("pull" in call or "stop" in call for call in self.calls))
        self.assertEqual(self.compose.read_text(), self.original)

    def test_adapter_blocks(self):
        (self.data / "hyperhive-upstream-adapter").mkdir()
        with self.assertRaisesRegex(RuntimeError, "Upstream adapter"):
            updater.update()

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
