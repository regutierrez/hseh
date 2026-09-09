#!/usr/bin/env python3
"""Isolated hosted mouse click + 100-column preview. Writes docs/evidence/v9/."""

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
ROWS, COLS = 40, 120


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


def acc_drain(fd, timeout, acc: bytearray):
    chunks = []
    drain(fd, timeout, chunks)
    for chunk in chunks:
        acc.extend(chunk)


class CellScreen:
    def __init__(self, rows=ROWS, cols=COLS):
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


def reconstruct(acc: bytearray) -> CellScreen:
    scr = CellScreen()
    scr.feed(bytes(acc).decode("utf-8", "replace"))
    return scr


def popup_origin(scr: CellScreen) -> int:
    for r in range(scr.rows):
        row = scr.row(r)
        nxt = scr.row(min(r + 1, scr.rows - 1)) + row
        if "hseh" in row and "spaces" in nxt:
            return row.find("hseh")
    return 0


def popup_preview_text(scr: CellScreen) -> str:
    start = popup_origin(scr)
    body = []
    for r in range(scr.rows):
        row = scr.row(r)
        if start + 20 < len(row):
            body.append(row[start + 20 :])
    return "\n".join(body)


def run_direct_popup(env: dict, cols: int) -> str:
    pid, fd = pty.fork()
    if pid == 0:
        os.environ.clear()
        os.environ.update(env)
        fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack("HHHH", 24, cols, 0, 0))
        os.execvp(str(HSEH), [str(HSEH), "popup", "spaces"])
    try:
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 24, cols, 0, 0))
        acc = bytearray()
        deadline = time.time() + 6
        while time.time() < deadline:
            acc_drain(fd, 0.3, acc)
            if b"alpha" in acc or b"beta" in acc or b"hseh" in acc:
                break
        acc_drain(fd, 1.5, acc)
        try:
            os.write(fd, b"\x1b")
        except OSError:
            pass
        acc_drain(fd, 0.3, acc)
        scr = CellScreen(24, cols)
        scr.feed(bytes(acc).decode("utf-8", "replace"))
        return scr.dump()
    finally:
        try:
            os.close(fd)
        except OSError:
            pass
        try:
            os.kill(pid, 15)
        except OSError:
            pass
        try:
            os.waitpid(pid, 0)
        except ChildProcessError:
            pass


def list_hit(scr: CellScreen, label: str, want_selected: bool | None = None):
    start = popup_origin(scr)
    list_end = start + max(40, (scr.cols - start) // 2)
    for r in range(scr.rows):
        row = scr.row(r)
        idx = row.find(label)
        if idx < 0 or idx >= list_end:
            continue
        prefix = row[max(start, idx - 4) : idx]
        selected = ">" in prefix
        if want_selected is None or selected is want_selected:
            return r, idx, selected
    return None


def sgr_left_click(fd, row0: int, col0: int):
    r, c = row0 + 1, col0 + 1
    os.write(fd, f"\x1b[<0;{c};{r}M".encode())
    os.write(fd, f"\x1b[<0;{c};{r}m".encode())


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
            raise SystemExit(f"isolated socket missing\n{slog.read_text()[-2000:]}")
        env = dict(base)
        env["HERDR_SOCKET_PATH"] = str(sock)
        env["HERDR_SESSION"] = "hseh-accept"

        def herdr(*args):
            r = run(env, ["herdr", *args])
            if r.returncode != 0:
                raise SystemExit(f"herdr {args} failed {r.returncode}: {r.stderr}{r.stdout}")
            return r

        a = json.loads(herdr("workspace", "create", "--label", "alpha", "--cwd", "/tmp").stdout)
        b = json.loads(herdr("workspace", "create", "--label", "beta", "--cwd", "/tmp", "--no-focus").stdout)
        pane_a = a["result"]["root_pane"]["pane_id"]
        pane_b = b["result"]["root_pane"]["pane_id"]
        ws_a = a["result"]["workspace"]["workspace_id"]
        ws_b = b["result"]["workspace"]["workspace_id"]
        herdr("pane", "run", pane_a, "sh -c 'while true; do echo ALPHA_MARK; sleep 1; done'")
        herdr("pane", "run", pane_b, "sh -c 'i=0; while true; do i=$((i+1)); echo BETA_TICK_$i; sleep 1; done'")
        herdr("plugin", "link", str(ROOT))
        herdr("workspace", "focus", ws_a)
        time.sleep(1.2)
        dump99 = run_direct_popup(env, 99)
        dump100 = run_direct_popup(env, 100)
        (OUT / "popup-99-dump.txt").write_text(dump99)
        (OUT / "popup-100-dump.txt").write_text(dump100)
        if "ALPHA_MARK" in dump99 or "BETA_TICK" in dump99:
            raise SystemExit(f"99-column popup content showed preview\n{dump99}")
        if "ALPHA_MARK" not in dump100 and "BETA_TICK" not in dump100:
            raise SystemExit(f"100-column popup content hid preview\n{dump100}")
        cfg = Path(herdr("plugin", "config-dir", "hseh").stdout.strip())
        cfg.mkdir(parents=True, exist_ok=True)
        (cfg / "hseh.toml").write_text("preview_poll_ms = 500\nwide_preview_min_columns = 40\n")

        pid, fd = pty.fork()
        if pid == 0:
            os.environ.clear()
            os.environ.update(env)
            fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
            os.execvp("herdr", ["herdr", "--session", "hseh-accept"])
        child = pid
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        acc_pre = bytearray()
        acc_drain(fd, 1.2, acc_pre)
        herdr("plugin", "action", "invoke", "hseh.spaces")
        acc = bytearray()
        deadline = time.time() + 6
        while time.time() < deadline:
            acc_drain(fd, 0.4, acc)
            if "hseh" in reconstruct(acc).dump() and "spaces" in reconstruct(acc).dump():
                break
        acc_drain(fd, 1.0, acc)
        (OUT / "open.raw").write_bytes(bytes(acc))
        scr = reconstruct(acc)
        (OUT / "open-dump.txt").write_text(scr.dump())
        before = json.loads(herdr("workspace", "list").stdout)
        (OUT / "before-click.json").write_text(json.dumps(before, indent=2))
        if focused_ids(before) != [ws_a]:
            raise SystemExit(f"expected {ws_a} focused {focused_ids(before)}")
        if list_hit(scr, "beta", want_selected=False) is None:
            hit_a = list_hit(scr, "alpha", want_selected=False)
            if not hit_a:
                raise SystemExit(f"alpha already selected; cannot deselect beta\n{scr.dump()}")
            sgr_left_click(fd, hit_a[0], hit_a[1])
            acc_drain(fd, 1.2, acc)
            scr = reconstruct(acc)
            if not re.search(r">\s*.*alpha", scr.dump()):
                raise SystemExit(f"click did not select alpha\n{scr.dump()}")
            if focused_ids(json.loads(herdr("workspace", "list").stdout)) != [ws_a]:
                raise SystemExit("alpha click focused herdr")
        hit_b = list_hit(scr, "beta", want_selected=False)
        if not hit_b:
            raise SystemExit(f"beta missing or already selected\n{scr.dump()}")
        r, c, _ = hit_b
        sgr_left_click(fd, r, c)
        acc_drain(fd, 2.0, acc)
        (OUT / "after-click.raw").write_bytes(bytes(acc))
        after_scr = reconstruct(acc)
        (OUT / "after-click-dump.txt").write_text(after_scr.dump())
        dump = after_scr.dump()
        if not re.search(r">\s*.*beta", dump):
            raise SystemExit(f"click did not select beta\n{dump}")
        preview = popup_preview_text(after_scr)
        (OUT / "after-click-preview.txt").write_text(preview)
        if "BETA_TICK" not in preview:
            raise SystemExit(f"click did not show beta preview\n{preview}")
        mid = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-click-focus.json").write_text(json.dumps(mid, indent=2))
        if focused_ids(mid) != [ws_a]:
            raise SystemExit(f"beta click changed API focus {focused_ids(mid)}")
        os.write(fd, b"\x1b")
        acc_drain(fd, 0.8, acc)
        (OUT / "after-escape.raw").write_bytes(bytes(acc))
        esc = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-escape.json").write_text(json.dumps(esc, indent=2))
        if focused_ids(esc) != [ws_a]:
            raise SystemExit("escape changed focus")
        herdr("plugin", "action", "invoke", "hseh.spaces")
        enter_acc = bytearray()
        deadline = time.time() + 6
        while time.time() < deadline:
            acc_drain(fd, 0.4, enter_acc)
            if "hseh" in reconstruct(enter_acc).dump():
                break
        acc_drain(fd, 0.8, enter_acc)
        reopen_scr = reconstruct(enter_acc)
        if list_hit(reopen_scr, "beta", want_selected=False) is None:
            hit_a = list_hit(reopen_scr, "alpha", want_selected=False)
            if not hit_a:
                raise SystemExit(f"reopen cannot unselect beta\n{reopen_scr.dump()}")
            sgr_left_click(fd, hit_a[0], hit_a[1])
            acc_drain(fd, 1.0, enter_acc)
            reopen_scr = reconstruct(enter_acc)
        hit_b = list_hit(reopen_scr, "beta", want_selected=False)
        if not hit_b:
            raise SystemExit(f"reopen beta missing or already selected\n{reopen_scr.dump()}")
        sgr_left_click(fd, hit_b[0], hit_b[1])
        acc_drain(fd, 1.0, enter_acc)
        (OUT / "reopen-click.raw").write_bytes(bytes(enter_acc))
        (OUT / "reopen-click-dump.txt").write_text(reconstruct(enter_acc).dump())
        if not re.search(r">\s*.*beta", reconstruct(enter_acc).dump()):
            raise SystemExit(f"reopen click did not select beta\n{reconstruct(enter_acc).dump()}")
        pre_enter = json.loads(herdr("workspace", "list").stdout)
        (OUT / "reopen-click-focus.json").write_text(json.dumps(pre_enter, indent=2))
        if focused_ids(pre_enter) != [ws_a]:
            raise SystemExit(f"beta click before Enter focused {focused_ids(pre_enter)}")
        os.write(fd, b"\r")
        acc_drain(fd, 1.5, enter_acc)
        time.sleep(0.4)
        after_enter = json.loads(herdr("workspace", "list").stdout)
        (OUT / "after-enter.json").write_text(json.dumps(after_enter, indent=2))
        if focused_ids(after_enter) != [ws_b]:
            raise SystemExit(f"enter focus {focused_ids(after_enter)} want {[ws_b]}")
        after_plugins = sha256(USER_PLUGINS)
        if after_plugins != before_plugins:
            raise SystemExit("user plugins.json changed")
        (OUT / "notes.txt").write_text(
            f"iso={iso}\nclick={r},{c}\nws_a={ws_a} ws_b={ws_b}\n"
            f"after_click_focus={focused_ids(mid)}\nafter_enter={focused_ids(after_enter)}\n"
            f"plugins={before_plugins}\n"
        )
        print("PASS mouse", "click", (r, c), "enter", focused_ids(after_enter))
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


if __name__ == "__main__":
    raise SystemExit(main())
