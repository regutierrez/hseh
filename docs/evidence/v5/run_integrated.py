#!/usr/bin/env python3
"""Isolated Herdr client + hseh popup. Writes only under docs/evidence/v5/."""

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


def run(env, args, timeout=30):
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


def strip_csi(text: str) -> str:
    text = re.sub(r"\x1b\[[0-9;?]*[A-Za-z]", "", text)
    text = re.sub(r"\x1b\].*?(?:\x07|\x1b\\)", "", text, flags=re.S)
    return text.replace("\x1b", "")


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
    text = screen.dump()
    # Popup title "hseh" is drawn inside the Herdr modal, not column 0.
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


def main() -> int:
    OUT.mkdir(parents=True, exist_ok=True)
    before_plugins = sha256(USER_PLUGINS)
    iso = Path(subprocess.check_output(["mktemp", "-d", "/tmp/hseh-accept-XXXXXX"], text=True).strip())
    sock = iso / ".config/herdr/sessions/hseh-accept/herdr.sock"
    server = None
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
        for _ in range(30):
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

        a = json.loads(herdr("workspace", "create", "--label", "alpha", "--cwd", "/tmp", "--no-focus").stdout)
        b = json.loads(herdr("workspace", "create", "--label", "beta", "--cwd", "/tmp", "--no-focus").stdout)
        pane_a = a["result"]["root_pane"]["pane_id"]
        pane_b = b["result"]["root_pane"]["pane_id"]
        herdr("pane", "run", pane_a, "sh -c 'while true; do echo ALPHA_MARK; sleep 1; done'")
        herdr("pane", "run", pane_b, "sh -c 'i=0; while true; do i=$((i+1)); echo BETA_TICK_$i; sleep 1; done'")
        herdr("plugin", "link", str(ROOT))
        cfg = iso / ".config/herdr/plugins/config/hseh"
        cfg.mkdir(parents=True, exist_ok=True)
        (cfg / "hseh.toml").write_text("preview_poll_ms = 500\nwide_preview_min_columns = 40\n")
        herdr("workspace", "focus", "w1")
        time.sleep(1.5)
        before = json.loads(herdr("workspace", "list").stdout)
        assert before["result"]["workspaces"][0]["focused"] is True
        (OUT / "before-focus.json").write_text(json.dumps(before, indent=2))

        pid, fd = pty.fork()
        if pid == 0:
            os.environ.clear()
            os.environ.update(env)
            fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
            os.execvp("herdr", ["herdr", "--session", "hseh-accept"])
        child = pid
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        pre = []
        drain(fd, 1.5, pre)
        invoke = herdr("plugin", "action", "invoke", "hseh.spaces")
        (OUT / "action-invoke.json").write_text(invoke.stdout)
        opened = []
        deadline = time.time() + 5
        while time.time() < deadline:
            drain(fd, 0.4, opened)
            if b"hseh" in b"".join(opened):
                break
        opened_bytes = b"".join(opened)
        if b"hseh" not in opened_bytes:
            (OUT / "popup-open-fail.raw").write_bytes(b"".join(pre + opened))
            raise SystemExit("popup did not render hseh in Herdr client")
        t_open = time.time()
        acc = bytearray(opened_bytes)
        (OUT / "popup-open.raw").write_bytes(opened_bytes)
        os.write(fd, b"\x1b[B")
        after_down = []
        drain(fd, 0.8, after_down)
        acc.extend(b"".join(after_down))
        frames = []
        for i in range(4):
            chunk = []
            drain(fd, 1.1, chunk)
            acc.extend(b"".join(chunk))
            raw = bytes(acc)
            frames.append((time.time(), raw))
            (OUT / f"frame-beta-{i}.raw").write_bytes(raw)
        preview_parts = []
        for i, (ts, raw) in enumerate(frames):
            scr = CellScreen(30, 100)
            scr.feed(raw.decode("utf-8", "replace"))
            dump = scr.dump()
            (OUT / f"frame-dump-{i}.txt").write_text(dump)
            if not re.search(r">\s*·\s*·\s*beta", dump):
                raise SystemExit(f"frame {i} missing selected beta\n{dump}")
            preview_parts.append(popup_preview_text(scr))
        preview_text = "\n---\n".join(preview_parts)
        ticks = sorted(set(re.findall(r"BETA_TICK_\d+", preview_text)))
        (OUT / "preview-column.txt").write_text(preview_text)
        (OUT / "notes.txt").write_text(
            f"iso={iso}\nopen_ts={t_open}\nticks={ticks}\n"
            f"selection_after_down contains beta={b'beta' in b''.join(after_down + [frames[0][1]])}\n"
        )
        if len(ticks) < 2:
            raise SystemExit(f"preview column lacked two BETA_TICK values: {ticks}\n{preview_text[:1000]}")
        mid = json.loads(herdr("workspace", "list").stdout)
        if not mid["result"]["workspaces"][0]["focused"]:
            raise SystemExit(f"alpha lost focus while browsing: {mid}")
        (OUT / "during-browse.json").write_text(json.dumps(mid, indent=2))
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 20, 50, 0, 0))
        narrow = []
        drain(fd, 1.2, narrow)
        (OUT / "narrow.raw").write_bytes(b"".join(narrow))
        nscr = CellScreen(20, 50)
        nscr.feed(b"".join(narrow).decode("utf-8", "replace"))
        (OUT / "narrow-dump.txt").write_text(nscr.dump())
        if re.search(r"BETA_TICK_\d+", popup_preview_text(nscr)):
            raise SystemExit("narrow frame still shows preview ticks")
        if not re.search(r">\s*·\s*·\s*beta", nscr.dump()):
            raise SystemExit(f"narrow lost beta selection\n{nscr.dump()}")
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        resume = []
        drain(fd, 2.0, resume)
        rscr = CellScreen(30, 100)
        rscr.feed(b"".join(resume).decode("utf-8", "replace"))
        (OUT / "resume-wide-dump.txt").write_text(rscr.dump())
        if not re.search(r">\s*·\s*·\s*beta", rscr.dump()):
            raise SystemExit(f"resume lost beta selection\n{rscr.dump()}")
        if not re.search(r"BETA_TICK_\d+", popup_preview_text(rscr)):
            raise SystemExit("resume wide did not restore preview ticks")
        os.write(fd, b"\x1b")
        drain(fd, 0.8, [])
        after_esc = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-escape.json").write_text(json.dumps(after_esc, indent=2))
        if not after_esc["result"]["workspaces"][0]["focused"]:
            raise SystemExit("escape changed focus")
        invoke2 = herdr("plugin", "action", "invoke", "hseh.spaces")
        opened2 = []
        deadline2 = time.time() + 5
        while time.time() < deadline2:
            drain(fd, 0.4, opened2)
            if b"hseh" in b"".join(opened2):
                break
        os.write(fd, b"\x1b[B")
        drain(fd, 0.6, [])
        os.write(fd, b"\r")
        drain(fd, 1.2, [])
        after_enter = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-enter.json").write_text(json.dumps(after_enter, indent=2))
        focused = [w["workspace_id"] for w in after_enter["result"]["workspaces"] if w["focused"]]
        if focused != ["w2"]:
            raise SystemExit(f"enter did not focus beta w2: {focused}")
        print("PASS ticks", ticks, "enter", focused)
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
