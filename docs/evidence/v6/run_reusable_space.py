#!/usr/bin/env python3
"""Isolated Herdr create-or-focus. Writes only under docs/evidence/v6/."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
HSEH = ROOT / "hseh"
OUT = Path(__file__).resolve().parent
USER_PLUGINS = Path.home() / ".config/herdr/plugins.json"


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(env, args, timeout=30):
    return subprocess.run(args, env=env, capture_output=True, text=True, timeout=timeout)


def main() -> int:
    OUT.mkdir(parents=True, exist_ok=True)
    before_plugins = sha256(USER_PLUGINS)
    iso = Path(subprocess.check_output(["mktemp", "-d", "/tmp/hseh-accept-XXXXXX"], text=True).strip())
    sock = iso / ".config/herdr/sessions/hseh-accept/herdr.sock"
    server = None
    try:
        (iso / ".config/herdr").mkdir(parents=True)
        (iso / ".local/state").mkdir(parents=True)
        (iso / ".cache").mkdir(parents=True)
        (iso / ".local/share").mkdir(parents=True)
        (iso / ".config/herdr/config.toml").write_text(
            "onboarding = false\n[experimental]\nallow_nested = true\n"
        )
        proj = iso / "proj"
        web = proj / "web"
        app = web / "app"
        app.mkdir(parents=True)
        marker = iso / "marker.txt"
        definition_id = "def-iso-1"
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
        slog = iso / "server.log"
        server = subprocess.Popen(
            ["herdr", "--session", "hseh-accept", "server"],
            env=base,
            stdout=open(slog, "w"),
            stderr=subprocess.STDOUT,
        )
        for _ in range(40):
            if sock.exists():
                break
            time.sleep(0.1)
        if not sock.exists():
            raise SystemExit(f"isolated socket missing\n{slog.read_text()}")
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
        spaces = cfg_dir / "spaces"
        spaces.mkdir(parents=True, exist_ok=True)
        (spaces / "one.toml").write_text(
            f'id = "{definition_id}"\n'
            'name = "iso-one"\n'
            f'working_dir = "{proj}"\n'
            "[[tabs]]\n"
            'name = "root"\n'
            f'command = "echo HSEH_MARK >> {marker}"\n'
            "[[tabs]]\n"
            'name = "web"\n'
            'working_dir = "web"\n'
            "[[tabs.panes]]\n"
            "[[tabs.panes]]\n"
            'split = "right"\n'
            'working_dir = "web/app"\n'
        )
        state_dir = iso / ".local/state/hseh"
        state_dir.mkdir(parents=True, exist_ok=True)
        open_env = dict(env)
        open_env["HERDR_PLUGIN_CONFIG_DIR"] = str(cfg_dir)
        open_env["HERDR_PLUGIN_STATE_DIR"] = str(state_dir)

        def hseh_open():
            r = run(open_env, [str(HSEH), "open", definition_id], timeout=40)
            (OUT / "open-output.txt").write_text((r.stdout or "") + (r.stderr or ""))
            if r.returncode != 0:
                raise SystemExit(f"hseh open failed {r.returncode}: {r.stderr}{r.stdout}")
            return r

        first = hseh_open()
        (OUT / "open-1.txt").write_text(first.stdout)
        time.sleep(0.6)
        listed = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-open-1.json").write_text(json.dumps(listed, indent=2))
        focused = [w for w in listed["result"]["workspaces"] if w.get("focused")]
        if not focused:
            raise SystemExit(f"no focused workspace after create: {listed}")
        created_id = focused[0]["workspace_id"]
        tabs = json.loads(herdr("tab", "list").stdout)
        (OUT / "tabs-after-create.json").write_text(json.dumps(tabs, indent=2))
        panes = json.loads(herdr("pane", "list").stdout)
        (OUT / "panes-after-create.json").write_text(json.dumps(panes, indent=2))
        tab_labels = sorted(
            t["label"]
            for t in tabs["result"]["tabs"]
            if t["workspace_id"] == created_id
        )
        if "root" not in tab_labels or "web" not in tab_labels:
            raise SystemExit(f"missing tabs {tab_labels}")
        pane_cwds = {
            p["pane_id"]: p.get("cwd")
            for p in panes["result"]["panes"]
            if p["workspace_id"] == created_id
        }
        if str(proj) not in pane_cwds.values():
            raise SystemExit(f"root cwd missing: {pane_cwds}")
        if str(app) not in pane_cwds.values():
            raise SystemExit(f"split cwd missing: {pane_cwds}")
        marks = marker.read_text() if marker.exists() else ""
        if marks.count("HSEH_MARK") != 1:
            raise SystemExit(f"marker count after first open: {marks!r}")
        second = hseh_open()
        (OUT / "open-2.txt").write_text(second.stdout)
        if "focus" not in second.stdout:
            raise SystemExit(f"second open was not focus: {second.stdout}")
        marks2 = marker.read_text()
        if marks2.count("HSEH_MARK") != 1:
            raise SystemExit(f"marker replayed: {marks2!r}")
        listed2 = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-open-2.json").write_text(json.dumps(listed2, indent=2))
        focused2 = [w["workspace_id"] for w in listed2["result"]["workspaces"] if w.get("focused")]
        if focused2 != [created_id]:
            raise SystemExit(f"second open focused {focused2} want {[created_id]}")
        after_plugins = sha256(USER_PLUGINS)
        if after_plugins != before_plugins:
            raise SystemExit("user plugins.json changed")
        (OUT / "notes.txt").write_text(
            f"iso={iso}\ncreated={created_id}\nmarker={marks2}\n"
            f"tabs={tab_labels}\ncwds={pane_cwds}\n"
            f"open1={first.stdout.strip()}\nopen2={second.stdout.strip()}\n"
            f"plugins={before_plugins}\n"
        )
        print("PASS", created_id, "marker", marks2.count("HSEH_MARK"), "focus", focused2)
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
        after_plugins = sha256(USER_PLUGINS)
        if after_plugins != before_plugins:
            print("WARNING user plugins.json changed", file=sys.stderr)


if __name__ == "__main__":
    raise SystemExit(main())
