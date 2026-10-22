#!/usr/bin/env python3
"""Update only NPM. Stop hyperhive-master first; restart it after testing.

Usage: sudo python3 scripts/update_npm.py
       sudo python3 scripts/update_npm.py --rollback /path/to/backup
Backups include private keys. Keep the printed backup directory private.
The target image is read from master/npm/npm.go; edit its image constant only.
With the master stopped, use --reset-upstreams to delete pools and pooled
proxy hosts/streams. Direct-target resources and certificates are preserved.
An older master binary is optional. If unavailable, restore the older Git
revision and rebuild the master after rolling back NPM, before restarting PM2.
"""

import argparse
from contextlib import closing
import datetime
import json
import os
from pathlib import Path
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time


def target_image():
    source = Path(__file__).resolve().parents[1] / "master/npm/npm.go"
    matches = re.findall(r'^\s*image\s*=\s*"(jc21/nginx-proxy-manager:[\w.-]+)"', source.read_text(), re.MULTILINE)
    if len(matches) != 1:
        raise RuntimeError("Expected one NPM image constant in " + str(source))
    return matches[0]


def run(*args, capture=False):
    result = subprocess.run(args, check=True, text=True, timeout=600,
                            stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE if capture else None)
    return result.stdout if capture else None


def replace_image(content, image):
    # Only the single-service, literal-image Compose layout is supported.
    pattern = r"(?m)^(\s+image:\s*)[^\n#]+?(\s*(?:#[^\n]*)?)$"
    if len(re.findall(pattern, content)) != 1:
        raise RuntimeError("Expected exactly one literal image: line; no files changed.")
    return re.sub(pattern, lambda m: m[1] + image + m[2], content)


def atomic_write(path, content):
    mode = path.stat().st_mode & 0o777
    with tempfile.NamedTemporaryFile(mode="w", dir=path.parent, delete=False) as stream:
        temporary = Path(stream.name)
        stream.write(content)
    os.chmod(temporary, mode)
    os.chown(temporary, path.stat().st_uid, path.stat().st_gid)
    os.replace(temporary, path)


def adapter_manifest():
    binary = Path(__file__).resolve().parents[1] / "master/512SvMan"
    if not binary.is_file() or b"--npm-upgrade-manifest" not in binary.read_bytes():
        raise RuntimeError("Build the updated master first: cd master && go build")
    manifest = json.loads(run(str(binary), "--npm-upgrade-manifest", capture=True))
    if manifest.get("target") != target_image():
        raise RuntimeError("Master binary is outdated. Rebuild it with: cd master && go build")
    return manifest


def upstream_state(work, reset=False):
    database = work / "data.db"
    if not database.exists():
        return False
    with closing(sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)) as connection:
        tables = {row[0] for row in connection.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        pending = "npm_upstream_operations" in tables and connection.execute("SELECT 1 FROM npm_upstream_operations WHERE state NOT IN ('applied','rolled_back') LIMIT 1").fetchone() is not None
        if pending and not reset:
            raise RuntimeError("Unfinished upstream operation: reconcile with the current NPM before upgrading.")
        return pending or ("npm_upstreams" in tables and connection.execute("SELECT 1 FROM npm_upstreams LIMIT 1").fetchone() is not None)


def verify_adapter_mounts(container, data, templates):
    mounts = {item["Destination"]: item for item in container["Mounts"]}
    for name, template in templates.items():
        mount = mounts.get(template["path"])
        if mount and (mount["Type"] != "bind" or Path(mount["Source"]).resolve() != data / "hyperhive-upstream-adapter" / name):
            raise RuntimeError("Custom template mount conflicts with the adapter: " + name)


def reset_upstreams(work, data):
    """Called only after NPM is stopped and all databases/data are backed up."""
    deleted = {"proxy_host": set(), "stream": set()}
    database = work / "data.db"
    with closing(sqlite3.connect(database)) as connection:
        tables = {row[0] for row in connection.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        if "npm_upstream_bindings" in tables:
            for kind, identifier in connection.execute("SELECT consumer_type, npm_id FROM npm_upstream_bindings"):
                deleted["stream" if kind == "stream" else "proxy_host"].add(identifier)
        with closing(sqlite3.connect(data / "database.sqlite")) as npm:
            # Inspect actual targets too, including pools created before bindings were persisted.
            for table, column in (("proxy_host", "forward_host"), ("stream", "forwarding_host")):
                for identifier, host in npm.execute(f"SELECT id, {column} FROM {table} WHERE is_deleted = 0"):
                    if re.fullmatch(r"hh_upstream_[0-9]+", host or ""):
                        deleted[table].add(identifier)
            for identifier, locations in npm.execute("SELECT id, locations FROM proxy_host WHERE is_deleted = 0"):
                if any(re.fullmatch(r"hh_upstream_[0-9]+", location.get("forward_host", "")) for location in json.loads(locations or "[]")):
                    deleted["proxy_host"].add(identifier)
            with npm:
                for table, identifiers in deleted.items():
                    for identifier in identifiers:
                        npm.execute(f"UPDATE {table} SET is_deleted = 1, enabled = 0 WHERE id = ?", (identifier,))
        with connection:
            for table in ("npm_upstream_bindings", "npm_upstream_operations", "npm_upstreams"):
                if table in tables:
                    connection.execute("DELETE FROM " + table)
            if "resource_descriptions" in tables:
                for identifier in deleted["stream"]:
                    connection.execute("DELETE FROM resource_descriptions WHERE resource_type = 'npm_stream' AND resource_id = ?", (identifier,))
    for table, identifiers in deleted.items():
        for identifier in identifiers:
            for path in (data / "nginx" / table).glob(str(identifier) + ".conf*"):
                path.unlink()
    for hook, context in (("http_top", "http"), ("stream", "stream")):
        path = data / "nginx/custom" / (hook + ".conf")
        if path.exists():
            content = path.read_text().replace("# HyperHive managed upstream pools\n", "")
            content = content.replace(f"include /data/nginx/custom/hyperhive/current/{context}.conf;", "")
            atomic_write(path, content)
    for directory in (data / "nginx/custom/hyperhive", data / "hyperhive-upstream-adapter"):
        if directory.exists():
            shutil.rmtree(directory)
    print(f"Removed {len(deleted['proxy_host'])} pooled proxy hosts and {len(deleted['stream'])} pooled streams.")


def compose(path, *args, override=None, capture=False):
    command = ["docker", "compose", "--project-directory", str(path.parent), "-f", str(path)]
    if override:
        command += ["-f", str(override)]
    return run(*command, *args, capture=capture)


def wait_ready():
    for attempt in range(60):
        try:
            run("docker", "exec", "npm", "nginx", "-t", capture=True)
            run("docker", "exec", "npm", "node", "-e", "fetch('http://127.0.0.1:81/api/',{signal:AbortSignal.timeout(2000)}).then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))", capture=True)
            return
        except subprocess.CalledProcessError:
            if attempt == 59:
                raise RuntimeError("NPM did not become ready after 60 checks; use the printed rollback command.")
            time.sleep(1)


def update(reset=False):
    target = target_image()
    manifest = adapter_manifest()
    container = json.loads(run("docker", "inspect", "npm", capture=True))[0]
    labels = container["Config"].get("Labels") or {}
    work = Path(labels.get("com.docker.compose.project.working_dir", "")).resolve()
    files = labels.get("com.docker.compose.project.config_files", "").split(",")
    if not files[0]:
        raise RuntimeError("Missing NPM Compose file label.")
    path = Path(files[0])
    if not path.is_absolute():
        path = work / path
    path = path.resolve(strict=True)
    if path.parent != work:
        raise RuntimeError("Compose file must be in the NPM working directory.")
    for name in ("compose.override.yaml", "compose.override.yml", "docker-compose.override.yml", "docker-compose.override.yaml"):
        if (work / name).exists():
            raise RuntimeError("Compose override detected; review it manually before upgrading.")
    override_path = work / "docker-compose.hyperhive-upstreams.yml"
    override = override_path if override_path.exists() else None
    allowed = {path, override_path}
    if any((Path(name) if Path(name).is_absolute() else work / name).resolve() not in allowed for name in files):
        raise RuntimeError("Unknown Compose override; review it before upgrading.")
    config = json.loads(compose(path, "config", "--format", "json", override=override, capture=True))
    services = config["services"]
    service = labels.get("com.docker.compose.service")
    if len(services) != 1 or service not in services:
        raise RuntimeError("Expected a single-service NPM Compose project.")
    old_image = services[service].get("image", "")
    if not re.fullmatch(r"jc21/nginx-proxy-manager(?::[\w.-]+|@sha256:[0-9a-f]{64})", old_image):
        raise RuntimeError("Expected an official NPM image in Compose.")
    selected_id = run("docker", "image", "inspect", "--format", "{{.Id}}", old_image, capture=True).strip()
    if selected_id != container["Image"]:
        raise RuntimeError("Compose and the running NPM image differ; resolve this before upgrading.")
    if container["Image"] == manifest["target_id"] and not reset:
        print("Compose and the container already select " + target)
        return
    environment = services[service].get("environment") or {}
    if any(key.startswith(("DB_MYSQL", "DB_POSTGRES")) for key in environment):
        raise RuntimeError("External NPM databases require a separate coordinated backup.")
    mounts = {mount["Destination"]: mount for mount in container["Mounts"]}
    if any(target.startswith(("/data/", "/etc/letsencrypt/")) for target in mounts):
        raise RuntimeError("Nested NPM data mounts need a separate coordinated backup.")
    directories = []
    for destination in ("/data", "/etc/letsencrypt"):
        mount = mounts.get(destination, {})
        if mount.get("Type") != "bind":
            raise RuntimeError("Expected bind-mounted NPM data and certificates.")
        directory = Path(mount["Source"]).resolve(strict=True)
        if not directory.is_dir() or directory == Path("/") or directory == work or directory in work.parents:
            raise RuntimeError("Unsafe NPM data mount: " + str(directory))
        directories.append(directory)
    if directories[0] == directories[1] or any(a in b.parents for a, b in (directories, directories[::-1])):
        raise RuntimeError("Overlapping NPM data mounts are unsupported.")
    pools = upstream_state(work, reset)
    adapter = bool(override or pools or (directories[0] / "hyperhive-upstream-adapter").exists() or (directories[0] / "nginx/custom/hyperhive").exists())
    if adapter:
        if directories[0] != work / "npm-data" or not (work / "data.db").exists():
            raise RuntimeError("Adapter migration requires the standard npm-data mount and HyperHive data.db.")
        if not reset:
            raise RuntimeError("Existing upstreams/adapter require --reset-upstreams, which deletes pooled proxy hosts, pooled streams and pools after backup.")
        previous_master = work / "512SvMan.npm-previous"
        if not (directories[0] / "database.sqlite").is_file():
            raise RuntimeError("NPM SQLite database is missing; cannot reset upstream resources.")
        templates = manifest["templates"]
        verify_adapter_mounts(container, directories[0], templates)
        if override:
            base = json.loads(compose(path, "config", "--format", "json", capture=True))
            left, right = dict(base["services"][service]), dict(services[service])
            for value in (left, right):
                value.pop("image", None)
                value["volumes"] = [v for v in value.get("volumes", []) if v["target"] not in {t["path"] for t in templates.values()}]
            if left != right:
                raise RuntimeError("Adapter override changes unrelated settings; review it manually.")
    original = path.read_text()
    updated = replace_image(original, target)
    run("docker", "pull", target)
    identity = run("docker", "image", "inspect", "--format", "{{.Id}}", target, capture=True).strip()
    if identity != manifest["target_id"]:
        raise RuntimeError("Target image identity is unverified for this architecture or the tag changed.")
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    backup = work / "npm-update-backups" / stamp
    backup.mkdir(parents=True, mode=0o700)
    if any(backup == directory or directory in backup.parents for directory in directories):
        raise RuntimeError("Backup directory must be outside NPM data mounts.")
    rollback_image = "hyperhive-npm-rollback:" + stamp.lower()
    run("docker", "image", "tag", container["Image"], rollback_image)
    (backup / "compose.original").write_text(original)
    if override:
        (backup / "override.original").write_bytes(override.read_bytes())
    if adapter and previous_master.is_file():
        shutil.copy2(previous_master, backup / "master.original")
    print("Backup: " + str(backup), flush=True)
    compose(path, "stop", service, override=override)
    try:
        for index, directory in enumerate(directories):
            run("tar", "--acls", "--xattrs", "-cpf", str(backup / f"data-{index}.tar"), "-C", str(directory), ".")
        database = work / "data.db"
        if database.exists():
            with closing(sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)) as source, closing(sqlite3.connect(backup / "hyperhive.db")) as destination:
                source.backup(destination)
        state = {"compose": str(path), "service": service, "directories": list(map(str, directories)), "old_image": old_image, "rollback_image": rollback_image, "adapter": adapter, "override": str(override_path), "had_override": override is not None}
        (backup / "state.json").write_text(json.dumps(state, indent=2))
        print("Rollback: sudo python3 " + str(Path(__file__).resolve()) + " --rollback " + str(backup), flush=True)
    except BaseException:
        compose(path, "start", service, override=override)
        raise
    atomic_write(path, updated)
    if adapter:
        reset_upstreams(work, directories[0])
        if override_path.exists():
            override_path.unlink()
        override = None
    compose(path, "config", "--quiet", override=override)
    compose(path, "up", "-d", "--no-deps", "--force-recreate", "--pull", "never", service, override=override)
    wait_ready()
    print("NPM now selects " + target + ". Test routing and certificate renewal before restarting the master.")
    if adapter:
        print("The reset removed only pooled proxy hosts/streams and upstream state; direct targets remain.")


def rollback(backup):
    backup = backup.resolve(strict=True)
    state = json.loads((backup / "state.json").read_text())
    path = Path(state["compose"])
    original = (backup / "compose.original").read_text()
    run("docker", "image", "inspect", state["rollback_image"], capture=True)
    for index in range(len(state["directories"])):
        run("tar", "-tf", str(backup / f"data-{index}.tar"), capture=True)
    override_path = Path(state.get("override", path.parent / "docker-compose.hyperhive-upstreams.yml"))
    override = override_path if override_path.exists() else None
    compose(path, "stop", state["service"], override=override)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    for index, name in enumerate(state["directories"]):
        directory = Path(name)
        preserved = directory.with_name(directory.name + ".before-rollback-" + stamp)
        directory.rename(preserved)
        directory.mkdir(mode=0o700)
        print("Preserved newer data: " + str(preserved), flush=True)
        run("tar", "--acls", "--xattrs", "-xpf", str(backup / f"data-{index}.tar"), "-C", str(directory))
    if "@" not in state["old_image"]:
        run("docker", "image", "tag", state["rollback_image"], state["old_image"])
    if (backup / "hyperhive.db").exists():
        database = path.parent / "data.db"
        for suffix in ("", "-wal", "-shm"):
            current = Path(str(database) + suffix)
            if current.exists():
                current.rename(current.with_name(current.name + ".before-rollback-" + stamp))
        shutil.copy2(backup / "hyperhive.db", database)
    if state.get("had_override"):
        override_path.write_bytes((backup / "override.original").read_bytes())
        override = override_path
    else:
        if override_path.exists():
            override_path.rename(override_path.with_name(override_path.name + ".before-rollback-" + stamp))
        override = None
    if (backup / "master.original").exists():
        executable = path.parent / "512SvMan"
        temporary = executable.with_name("512SvMan.rollback-tmp")
        shutil.copy2(backup / "master.original", temporary)
        os.replace(temporary, executable)
    atomic_write(path, original)
    compose(path, "up", "-d", "--no-deps", "--force-recreate", "--pull", "never", state["service"], override=override)
    wait_ready()
    print("Restored " + state["old_image"] + " and its pre-upgrade data. Restart the master when ready.")
    print("Restore a compatible Git revision and rebuild the master before restarting PM2 if needed.")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--reset-upstreams", action="store_true", help="Delete existing pools and their proxy hosts/streams after backing up; preserve direct targets.")
    parser.add_argument("--rollback", type=Path, metavar="BACKUP_DIRECTORY")
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error("Run with sudo. Stop hyperhive-master before updating or rolling back.")
    os.umask(0o077)
    try:
        rollback(args.rollback) if args.rollback else update(args.reset_upstreams)
    except (OSError, ValueError, RuntimeError, sqlite3.Error, subprocess.SubprocessError) as error:
        print("NPM update failed: " + str(error), file=sys.stderr)
        print("If a complete backup was printed, use its rollback command. Keep the master stopped until resolved.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
