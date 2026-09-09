#!/usr/bin/env python3
"""Isolated cold-restart recovery. Writes docs/evidence/v10/."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
HSEH = ROOT / "hseh"
OUT = Path(__file__).resolve().parent
USER_PLUGINS = Path.home() / ".config/herdr/plugins.json"


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(env, args, timeout=40):
    return subprocess.run(args, env=env, capture_output=True, text=True, timeout=timeout)


def focused_ids(listed):
    return [w["workspace_id"] for w in listed["result"]["workspaces"] if w.get("focused")]


def workspaces_by_label(listed):
    out = {}
    for w in listed["result"]["workspaces"]:
        out.setdefault(w.get("label"), []).append(w["workspace_id"])
    return out


def marker_count(path: Path, token: str) -> int:
    if not path.exists():
        return 0
    return path.read_text().count(token)


def start_server(base, slog: Path, sock: Path):
    proc = subprocess.Popen(
        ["herdr", "--session", "hseh-accept", "server"],
        env=base,
        stdout=open(slog, "a"),
        stderr=subprocess.STDOUT,
    )
    for _ in range(50):
        if sock.exists():
            return proc
        time.sleep(0.1)
    raise SystemExit(f"isolated socket missing\n{slog.read_text()[-2000:]}")


def wait_owned_server_stopped(proc, sock: Path, slog: Path):
    deadline = time.time() + 10
    while time.time() < deadline:
        living = proc.poll() is None
        if not sock.exists() and not living:
            return
        if not sock.exists() and living:
            time.sleep(0.1)
            continue
        time.sleep(0.1)
    raise SystemExit(
        f"owned server still present after stop sock={sock.exists()} pid_alive={proc.poll() is None}\n{slog.read_text()[-2000:]}"
    )


def load_associations(path: Path):
    raw = path.read_text()
    try:
        return json.loads(raw), raw
    except json.JSONDecodeError as err:
        raise SystemExit(f"associations json invalid {path}: {err}\n{raw}") from err


def parse_api_snapshot(raw: str):
    try:
        doc = json.loads(raw)
    except json.JSONDecodeError as err:
        raise SystemExit(f"herdr api snapshot is not JSON: {err}\n{raw}") from err
    result = doc.get("result", doc)
    if not isinstance(result, dict) or result.get("type") != "session_snapshot":
        raise SystemExit(f"herdr api snapshot missing session_snapshot\n{raw}")
    snapshot = result.get("snapshot")
    if not isinstance(snapshot, dict) or "workspaces" not in snapshot:
        raise SystemExit(f"herdr api snapshot missing snapshot.workspaces\n{raw}")
    return snapshot


def main() -> int:
    OUT.mkdir(parents=True, exist_ok=True)
    before_plugins = sha256(USER_PLUGINS)
    iso = Path(subprocess.check_output(["mktemp", "-d", "/tmp/hseh-accept-XXXXXX"], text=True).strip())
    sock = iso / ".config/herdr/sessions/hseh-accept/herdr.sock"
    proj1 = iso / "proj-one"
    proj2 = iso / "proj-two"
    proj1.mkdir(parents=True)
    proj2.mkdir(parents=True)
    mark1 = iso / "mark-one.txt"
    mark2 = iso / "mark-two.txt"
    slog = iso / "server.log"
    try:
        (iso / ".config/herdr").mkdir(parents=True)
        (iso / ".local/state").mkdir(parents=True)
        (iso / ".cache").mkdir(parents=True)
        (iso / ".local/share").mkdir(parents=True)
        (iso / ".config/herdr/config.toml").write_text(
            "onboarding = false\n[experimental]\nallow_nested = true\n"
        )
        base = {
            "HOME": str(iso),
            "XDG_CONFIG_HOME": str(iso / ".config"),
            "XDG_STATE_HOME": str(iso / ".local/state"),
            "XDG_CACHE_HOME": str(iso / ".cache"),
            "XDG_DATA_HOME": str(iso / ".local/share"),
            "PATH": os.environ["PATH"],
            "TERM": "xterm-256color",
            "LANG": os.environ.get("LANG", "C.UTF-8"),
        }
        server = start_server(base, slog, sock)
        env = dict(base)
        env["HERDR_SOCKET_PATH"] = str(sock)
        env["HERDR_SESSION"] = "hseh-accept"

        def herdr(*args):
            r = run(env, ["herdr", *args])
            if r.returncode != 0:
                raise SystemExit(f"herdr {args} failed {r.returncode}: {r.stderr}{r.stdout}")
            return r

        herdr("plugin", "link", str(ROOT))
        cfg_dir = Path(herdr("plugin", "config-dir", "hseh").stdout.strip())
        cfg_dir.mkdir(parents=True, exist_ok=True)
        spaces = cfg_dir / "spaces"
        spaces.mkdir(parents=True, exist_ok=True)
        (spaces / "one.toml").write_text(
            'id = "def-iso-1"\nname = "iso-one"\n'
            f'working_dir = "{proj1}"\n[[tabs]]\nname = "root"\n'
            f'command = "echo HSEH_MARK1 >> {mark1}"\n'
        )
        (spaces / "two.toml").write_text(
            'id = "def-iso-2"\nname = "iso-two"\n'
            f'working_dir = "{proj2}"\n[[tabs]]\nname = "root"\n'
            f'command = "echo HSEH_MARK2 >> {mark2}"\n'
        )
        cli_env = dict(env)
        for key in ("HERDR_PLUGIN_CONFIG_DIR", "HERDR_PLUGIN_STATE_DIR"):
            cli_env.pop(key, None)

        def hseh(*args):
            return run(cli_env, [str(HSEH), *args], timeout=40)

        first = hseh("open", "def-iso-1")
        (OUT / "open-1.txt").write_text(first.stdout + first.stderr)
        if first.returncode != 0:
            raise SystemExit(f"open 1 failed: {first.stderr}{first.stdout}")
        second = hseh("open", "def-iso-2")
        (OUT / "open-2.txt").write_text(second.stdout + second.stderr)
        if second.returncode != 0:
            raise SystemExit(f"open 2 failed: {second.stderr}{second.stdout}")
        before_restart = json.loads(herdr("workspace", "list").stdout)
        (OUT / "before-restart-list.json").write_text(json.dumps(before_restart, indent=2))
        assoc_path = iso / ".local/state/herdr/plugins/hseh/hseh-accept/associations.json"
        if not assoc_path.is_file():
            raise SystemExit(f"association file missing before restart: {assoc_path}")
        (OUT / "associations-before.json").write_text(assoc_path.read_text())
        herdr("server", "stop")
        wait_owned_server_stopped(server, sock, slog)
        server = start_server(base, slog, sock)
        env["HERDR_SOCKET_PATH"] = str(sock)
        cli_env["HERDR_SOCKET_PATH"] = str(sock)
        after_restart = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-restart-list.json").write_text(json.dumps(after_restart, indent=2))
        snap = herdr("api", "snapshot")
        (OUT / "after-restart-snapshot.json").write_text(snap.stdout)
        parse_api_snapshot(snap.stdout)
        base1 = marker_count(mark1, "HSEH_MARK1")
        base2 = marker_count(mark2, "HSEH_MARK2")
        (OUT / "marker-baseline.txt").write_text(f"mark1={base1}\nmark2={base2}\n")
        refused = hseh("open", "def-iso-1")
        (OUT / "open-after-restart.txt").write_text(refused.stdout + refused.stderr)
        if refused.returncode == 0 or "needs recovery" not in (refused.stderr + refused.stdout):
            raise SystemExit(f"open after restart should refuse: {refused.stdout}{refused.stderr}")
        if marker_count(mark1, "HSEH_MARK1") != base1:
            raise SystemExit("refused open mutated marker 1")
        labels = workspaces_by_label(after_restart)
        live_one = labels.get("iso-one")
        if not live_one:
            raise SystemExit(f"no restored iso-one workspace to reconnect\n{json.dumps(after_restart, indent=2)}")
        rec1 = hseh("recover", "def-iso-1", "--workspace", live_one[0])
        (OUT / "recover-1-workspace.txt").write_text(rec1.stdout + rec1.stderr)
        if rec1.returncode != 0:
            raise SystemExit(f"recover 1 failed: {rec1.stderr}{rec1.stdout}")
        if "reconnect" not in rec1.stdout:
            raise SystemExit(f"recover 1 action: {rec1.stdout}")
        if marker_count(mark1, "HSEH_MARK1") != base1:
            raise SystemExit(f"reconnect mutated marker 1 baseline {base1} now {marker_count(mark1, 'HSEH_MARK1')}")
        listed = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-reconnect.json").write_text(json.dumps(listed, indent=2))
        if focused_ids(listed) != [live_one[0]]:
            raise SystemExit(f"reconnect focus {focused_ids(listed)} want {live_one}")
        refused2 = hseh("open", "def-iso-2")
        (OUT / "open-2-after-reconnect.txt").write_text(refused2.stdout + refused2.stderr)
        if refused2.returncode == 0 or "needs recovery" not in (refused2.stderr + refused2.stdout):
            raise SystemExit(f"sibling open should still refuse: {refused2.stdout}{refused2.stderr}")
        after_reconnect_assoc, after_reconnect_raw = load_associations(assoc_path)
        (OUT / "associations-after-reconnect.json").write_text(after_reconnect_raw)
        current_ids = {r.get("definition_id") for r in after_reconnect_assoc.get("records") or []}
        unresolved_ids = {r.get("definition_id") for r in after_reconnect_assoc.get("unresolved") or []}
        if "def-iso-1" not in current_ids or "def-iso-2" in current_ids:
            raise SystemExit(f"after reconnect current {after_reconnect_assoc.get('records')}")
        if "def-iso-2" not in unresolved_ids:
            raise SystemExit(f"sibling unresolved missing after reconnect {after_reconnect_assoc.get('unresolved')}")
        rec2 = hseh("recover", "def-iso-2", "--create")
        (OUT / "recover-2-create.txt").write_text(rec2.stdout + rec2.stderr)
        if rec2.returncode != 0:
            raise SystemExit(f"recover 2 create failed: {rec2.stderr}{rec2.stdout}")
        time.sleep(0.5)
        if marker_count(mark2, "HSEH_MARK2") != base2 + 1:
            raise SystemExit(f"create marker 2 baseline {base2} now {marker_count(mark2, 'HSEH_MARK2')}")
        rec2b = hseh("recover", "def-iso-2", "--create")
        (OUT / "recover-2-create-repeat.txt").write_text(rec2b.stdout + rec2b.stderr)
        if rec2b.returncode != 0 or "focus" not in rec2b.stdout:
            raise SystemExit(f"repeat create should focus: {rec2b.stdout}{rec2b.stderr}")
        if marker_count(mark2, "HSEH_MARK2") != base2 + 1:
            raise SystemExit("repeat create replayed marker 2")
        after_create_assoc, after_create_raw = load_associations(assoc_path)
        (OUT / "associations-after-create.json").write_text(after_create_raw)
        current_ids = {r.get("definition_id") for r in after_create_assoc.get("records") or []}
        unresolved_ids = {r.get("definition_id") for r in after_create_assoc.get("unresolved") or []}
        if current_ids != {"def-iso-1", "def-iso-2"}:
            raise SystemExit(f"after create current {after_create_assoc.get('records')}")
        if unresolved_ids:
            raise SystemExit(f"unresolved left after both recoveries {after_create_assoc.get('unresolved')}")
        after_plugins = sha256(USER_PLUGINS)
        if after_plugins != before_plugins:
            raise SystemExit("user plugins.json changed")
        (OUT / "notes.txt").write_text(
            f"iso={iso}\nlive_one={live_one}\nbase1={base1} base2={base2}\n"
            f"mark1={marker_count(mark1, 'HSEH_MARK1')} mark2={marker_count(mark2, 'HSEH_MARK2')}\n"
            f"plugins={before_plugins}\n"
        )
        print("PASS recovery", "reconnect", live_one[0], "create-once", base2 + 1)
        return 0
    finally:
        if sock.exists():
            env = {
                "HOME": str(iso),
                "XDG_CONFIG_HOME": str(iso / ".config"),
                "XDG_STATE_HOME": str(iso / ".local/state"),
                "XDG_CACHE_HOME": str(iso / ".cache"),
                "XDG_DATA_HOME": str(iso / ".local/share"),
                "PATH": os.environ["PATH"],
                "HERDR_SOCKET_PATH": str(sock),
                "HERDR_SESSION": "hseh-accept",
            }
            run(env, ["herdr", "server", "stop"])
            time.sleep(0.4)
        shutil.rmtree(iso, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
