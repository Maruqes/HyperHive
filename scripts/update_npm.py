#!/usr/bin/env python3
"""Update only NPM. Stop hyperhive-master first; restart it after testing.

Usage: sudo python3 scripts/update_npm.py
       sudo python3 scripts/update_npm.py --rollback /path/to/backup
Backups include private keys. Keep the printed backup directory private.
The target image is read from master/npm/npm.go; edit its image constant only.
"""

import argparse
import datetime
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import tempfile


def target_image():
    source = Path(__file__).resolve().parents[1] / "master/npm/npm.go"
    matches = re.findall(r'^\s*image\s*=\s*"(jc21/nginx-proxy-manager:[\w.-]+)"', source.read_text(), re.MULTILINE)
    if len(matches) != 1:
        raise RuntimeError("Expected one NPM image constant in " + str(source))
    return matches[0]


def run(*args, capture=False):
    result = subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE if capture else None)
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


def check_upstreams(work, data):
    if (work / "docker-compose.hyperhive-upstreams.yml").exists() or (data / "hyperhive-upstream-adapter").exists() or (data / "nginx/custom/hyperhive").exists():
        raise RuntimeError("Upstream adapter detected: it requires NPM 2.13.5. Migrate it before upgrading.")
    database = work / "data.db"
    if database.exists():
        with sqlite3.connect(database.as_uri() + "?mode=ro", uri=True) as connection:
            exists = connection.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='npm_upstreams'").fetchone()
            if exists and connection.execute("SELECT 1 FROM npm_upstreams LIMIT 1").fetchone():
                raise RuntimeError("Saved upstream pools require NPM 2.13.5. Migrate them before upgrading.")


def compose(path, *args):
    return run("docker", "compose", "--project-directory", str(path.parent), "-f", str(path), *args)


def update():
    target = target_image()
    container = json.loads(run("docker", "inspect", "npm", capture=True))[0]
    labels = container["Config"].get("Labels") or {}
    work = Path(labels.get("com.docker.compose.project.working_dir", "")).resolve()
    files = labels.get("com.docker.compose.project.config_files", "").split(",")
    if len(files) != 1 or not files[0]:
        raise RuntimeError("Expected one existing NPM Compose file; overrides need manual review.")
    path = Path(files[0])
    if not path.is_absolute():
        path = work / path
    path = path.resolve(strict=True)
    if path.parent != work:
        raise RuntimeError("Compose file must be in the NPM working directory.")
    for name in ("compose.override.yaml", "compose.override.yml", "docker-compose.override.yml", "docker-compose.override.yaml"):
        if (work / name).exists():
            raise RuntimeError("Compose override detected; review it manually before upgrading.")
    config = json.loads(run("docker", "compose", "--project-directory", str(work), "-f", str(path), "config", "--format", "json", capture=True))
    services = config["services"]
    service = labels.get("com.docker.compose.service")
    if len(services) != 1 or service not in services:
        raise RuntimeError("Expected a single-service NPM Compose project.")
    old_image = services[service].get("image", "")
    if not re.fullmatch(r"jc21/nginx-proxy-manager:[\w.-]+", old_image) or old_image != container["Config"]["Image"]:
        raise RuntimeError("Expected a matching official NPM image tag in Compose and the container.")
    if old_image == target:
        print("Compose and the container already select " + target)
        return
    environment = services[service].get("environment") or {}
    if any(key.startswith(("DB_MYSQL", "DB_POSTGRES")) for key in environment):
        raise RuntimeError("External NPM databases require a separate coordinated backup.")
    mounts = {mount["Destination"]: mount for mount in container["Mounts"]}
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
    check_upstreams(work, directories[0])
    original = path.read_text()
    updated = replace_image(original, target)
    run("docker", "pull", target)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    backup = work / "npm-update-backups" / stamp
    backup.mkdir(parents=True, mode=0o700)
    if any(backup == directory or directory in backup.parents for directory in directories):
        raise RuntimeError("Backup directory must be outside NPM data mounts.")
    rollback_image = "hyperhive-npm-rollback:" + stamp.lower()
    run("docker", "image", "tag", container["Image"], rollback_image)
    (backup / "compose.original").write_text(original)
    print("Backup: " + str(backup), flush=True)
    print("Rollback: sudo python3 " + str(Path(__file__).resolve()) + " --rollback " + str(backup), flush=True)
    compose(path, "stop", service)
    try:
        for index, directory in enumerate(directories):
            run("tar", "--acls", "--xattrs", "-cpf", str(backup / f"data-{index}.tar"), "-C", str(directory), ".")
        state = {"compose": str(path), "service": service, "directories": list(map(str, directories)), "old_image": old_image, "rollback_image": rollback_image}
        (backup / "state.json").write_text(json.dumps(state, indent=2))
    except BaseException:
        compose(path, "start", service)
        raise
    atomic_write(path, updated)
    compose(path, "config", "--quiet")
    compose(path, "up", "-d", "--no-deps", "--pull", "never", service)
    print("NPM now selects " + target + ". Test routing and certificate renewal before restarting the master.")


def rollback(backup):
    backup = backup.resolve(strict=True)
    state = json.loads((backup / "state.json").read_text())
    path = Path(state["compose"])
    original = (backup / "compose.original").read_text()
    run("docker", "image", "inspect", state["rollback_image"], capture=True)
    for index in range(len(state["directories"])):
        run("tar", "-tf", str(backup / f"data-{index}.tar"), capture=True)
    compose(path, "stop", state["service"])
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    for index, name in enumerate(state["directories"]):
        directory = Path(name)
        preserved = directory.with_name(directory.name + ".before-rollback-" + stamp)
        directory.rename(preserved)
        directory.mkdir(mode=0o700)
        print("Preserved newer data: " + str(preserved), flush=True)
        run("tar", "--acls", "--xattrs", "-xpf", str(backup / f"data-{index}.tar"), "-C", str(directory))
    run("docker", "image", "tag", state["rollback_image"], state["old_image"])
    atomic_write(path, original)
    compose(path, "up", "-d", "--no-deps", "--pull", "never", state["service"])
    print("Restored " + state["old_image"] + " and its pre-upgrade data. Restart the master when ready.")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--rollback", type=Path, metavar="BACKUP_DIRECTORY")
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error("Run with sudo. Stop hyperhive-master before updating or rolling back.")
    os.umask(0o077)
    try:
        rollback(args.rollback) if args.rollback else update()
    except (OSError, ValueError, RuntimeError, sqlite3.Error, subprocess.CalledProcessError) as error:
        print("NPM update failed: " + str(error), file=sys.stderr)
        print("If a complete backup was printed, use its rollback command. Keep the master stopped until resolved.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
