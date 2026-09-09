#!/usr/bin/env python3
"""Isolated CLI/plugin parity. No HERDR_PLUGIN_* on CLI. Writes docs/evidence/v7/."""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
import pty
import re
import select
import shutil
import struct
import subprocess
import sys
import termios
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


def drain(fd, timeout, chunks):
    end = time.time() + timeout
    while time.time() < end:
        ready, _, _ = select.select([fd], [], [], 0.1)
        if not ready:
            continue
        try:
            data = os.read(fd, 8192)
        except OSError:
            break
        if not data:
            break
        chunks.append(data)
        if b"\x1b[6n" in data:
            try:
                os.write(fd, b"\x1b[24;80R")
            except OSError:
                break


class CellScreen:
    def __init__(self, rows=30, cols=100):
        self.rows, self.cols = rows, cols
        self.cells = [[" "] * cols for _ in range(rows)]
        self.r = 0
        self.c = 0

    def put(self, ch: str):
        if 0 <= self.r < self.rows and 0 <= self.c < self.cols:
            self.cells[self.r][self.c] = ch
        self.c += 1

    def feed(self, data: str):
        i = 0
        while i < len(data):
            if data[i] == "\x1b":
                if i + 1 < len(data) and data[i + 1] == "[":
                    j = i + 2
                    while j < len(data) and not ("@" <= data[j] <= "~"):
                        j += 1
                    if j >= len(data):
                        break
                    body, cmd = data[i + 2 : j], data[j]
                    nums = [int(p) if p.isdigit() else 0 for p in body.split(";") if p != ""]
                    if cmd == "H" or cmd == "f":
                        self.r = max(nums[0] - 1, 0) if nums else 0
                        self.c = max(nums[1] - 1, 0) if len(nums) > 1 else 0
                    elif cmd == "J":
                        self.cells = [[" "] * self.cols for _ in range(self.rows)]
                    i = j + 1
                    continue
                if i + 1 < len(data) and data[i + 1] == "]":
                    j = i + 2
                    while j < len(data) and data[j] not in "\x07":
                        if data[j] == "\x1b" and j + 1 < len(data) and data[j + 1] == "\\":
                            j += 2
                            break
                        j += 1
                    else:
                        j += 1
                    i = j
                    continue
                i += 2 if i + 1 < len(data) else 1
                continue
            if data[i] == "\n":
                self.r += 1
                self.c = 0
                i += 1
                continue
            if data[i] == "\r":
                self.c = 0
                i += 1
                continue
            self.put(data[i])
            i += 1

    def row(self, r):
        return "".join(self.cells[r])

    def dump(self):
        return "\n".join(self.row(r) for r in range(self.rows))


def popup_preview_text(screen: CellScreen) -> str:
    # Same origin detection as docs/evidence/v5/run_integrated.py.
    for r in range(screen.rows):
        row = screen.row(r)
        if "hseh" in row and "spaces" in screen.row(min(r + 1, screen.rows - 1)) + row:
            start = row.find("hseh")
            break
    else:
        start = 0
    body = []
    for r in range(screen.rows):
        row = screen.row(r)
        if start + 20 < len(row):
            body.append(row[start + 20 :])
    return "\n".join(body)


def focused_ids(listed):
    return [w["workspace_id"] for w in listed["result"]["workspaces"] if w.get("focused")]


def main() -> int:
    OUT.mkdir(parents=True, exist_ok=True)
    before_plugins = sha256(USER_PLUGINS)
    iso = Path(subprocess.check_output(["mktemp", "-d", "/tmp/hseh-accept-XXXXXX"], text=True).strip())
    sock = iso / ".config/herdr/sessions/hseh-accept/herdr.sock"
    fd = None
    child = None
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
        subprocess.Popen(
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
        cfg_dir.mkdir(parents=True, exist_ok=True)
        (cfg_dir / "hseh.toml").write_text("preview_poll_ms = 500\nwide_preview_min_columns = 40\n")
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
        cli_env = dict(env)
        for key in ("HERDR_PLUGIN_CONFIG_DIR", "HERDR_PLUGIN_STATE_DIR"):
            cli_env.pop(key, None)

        def hseh_open():
            r = run(cli_env, [str(HSEH), "open", definition_id], timeout=40)
            if r.returncode != 0:
                raise SystemExit(f"hseh open failed {r.returncode}: {r.stderr}{r.stdout}")
            return r

        def select_iso_one():
            os.write(fd, b"iso-one")
            drain(fd, 0.5, [])
            os.write(fd, b"\x1b[A")
            drain(fd, 0.8, [])

        pid, fd = pty.fork()
        if pid == 0:
            os.environ.clear()
            os.environ.update(env)
            fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
            os.execvp("herdr", ["herdr", "--session", "hseh-accept"])
        child = pid
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        drain(fd, 1.2, [])
        herdr("plugin", "action", "invoke", "hseh.spaces")
        opened = []
        deadline = time.time() + 6
        while time.time() < deadline:
            drain(fd, 0.4, opened)
            if b"iso-one" in b"".join(opened) or b"hseh" in b"".join(opened):
                break
        select_iso_one()
        preview_chunks = []
        drain(fd, 0.8, preview_chunks)
        acc = bytearray(b"".join(opened + preview_chunks))
        (OUT / "popup-preview.raw").write_bytes(bytes(acc))
        scr = CellScreen(30, 100)
        scr.feed(bytes(acc).decode("utf-8", "replace"))
        dump = scr.dump()
        (OUT / "popup-preview-dump.txt").write_text(dump)
        preview = popup_preview_text(scr)
        (OUT / "popup-preview.txt").write_text(preview)
        if "tab root" not in preview:
            raise SystemExit(f"preview column missing layout field 'tab root'\n{preview}\nDUMP\n{dump}")
        if marker.exists():
            raise SystemExit(f"marker existed before Enter: {marker.read_text()!r}")
        os.write(fd, b"\r")
        drain(fd, 2.0, [])
        time.sleep(0.6)
        marks = marker.read_text() if marker.exists() else ""
        (OUT / "marker-after-enter.txt").write_text(marks)
        if marks.count("HSEH_MARK") != 1:
            raise SystemExit(f"marker after popup enter: {marks!r}")
        listed = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-popup-enter.json").write_text(json.dumps(listed, indent=2))
        created = [w["workspace_id"] for w in listed["result"]["workspaces"] if w.get("label") == "iso-one"]
        if created != focused_ids(listed) or len(created) != 1:
            raise SystemExit(f"popup enter focused {focused_ids(listed)} iso-one {created}")
        created_id = created[0]
        tabs = json.loads(herdr("tab", "list").stdout)
        (OUT / "tabs-after-create.json").write_text(json.dumps(tabs, indent=2))
        panes = json.loads(herdr("pane", "list").stdout)
        (OUT / "panes-after-create.json").write_text(json.dumps(panes, indent=2))
        tab_labels = sorted(t["label"] for t in tabs["result"]["tabs"] if t["workspace_id"] == created_id)
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
        second = hseh_open()
        (OUT / "cli-open-2.txt").write_text(second.stdout)
        after_cli = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-cli-focus.json").write_text(json.dumps(after_cli, indent=2))
        if focused_ids(after_cli) != [created_id]:
            raise SystemExit(f"CLI focus {focused_ids(after_cli)} want {[created_id]}")
        iso_ws = [w["workspace_id"] for w in after_cli["result"]["workspaces"] if w.get("label") == "iso-one"]
        if iso_ws != [created_id]:
            raise SystemExit(f"iso-one workspaces {iso_ws}")
        if marker.read_text().count("HSEH_MARK") != 1:
            raise SystemExit("CLI reopen replayed marker")
        herdr("plugin", "action", "invoke", "hseh.spaces")
        drain(fd, 1.0, [])
        select_iso_one()
        os.write(fd, b"\r")
        drain(fd, 1.5, [])
        after_reopen = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-plugin-reopen.json").write_text(json.dumps(after_reopen, indent=2))
        if focused_ids(after_reopen) != [created_id]:
            raise SystemExit(f"plugin reopen focused {focused_ids(after_reopen)} want {[created_id]}")
        iso_ws2 = [w["workspace_id"] for w in after_reopen["result"]["workspaces"] if w.get("label") == "iso-one"]
        if iso_ws2 != [created_id]:
            raise SystemExit(f"duplicated iso-one: {iso_ws2}")
        if marker.read_text().count("HSEH_MARK") != 1:
            raise SystemExit("plugin reopen replayed marker")
        after_plugins = sha256(USER_PLUGINS)
        if after_plugins != before_plugins:
            raise SystemExit("user plugins.json changed")
        (OUT / "notes.txt").write_text(
            f"iso={iso}\ncreated={created_id}\ncli2={second.stdout.strip()}\n"
            f"after_cli_focus={focused_ids(after_cli)}\n"
            f"after_plugin_reopen={focused_ids(after_reopen)}\n"
            f"tabs={tab_labels}\ncwds={pane_cwds}\n"
            f"marker={marker.read_text().count('HSEH_MARK')}\nplugins={before_plugins}\n"
        )
        print("PASS", created_id, "marker 1", "focus", focused_ids(after_reopen))
        return 0
    finally:
        if fd is not None:
            try:
                os.close(fd)
            except OSError:
                pass
        if child is not None:
            try:
                os.waitpid(child, 0)
            except ChildProcessError:
                pass
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
