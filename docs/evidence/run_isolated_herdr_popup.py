#!/usr/bin/env python3
"""Drive an isolated Herdr client PTY and invoke the hseh popup action."""

import fcntl
import hashlib
import json
import os
import pty
import select
import shutil
import struct
import subprocess
import termios
import time
from pathlib import Path

HSEH = Path(__file__).resolve().parents[2] / "hseh"
EVIDENCE = Path(__file__).resolve().parent
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


def main():
    before = sha256(USER_PLUGINS)
    iso = Path(subprocess.check_output(["mktemp", "-d", "/tmp/hseh-accept-XXXXXX"], text=True).strip())
    (iso / ".config/herdr").mkdir(parents=True)
    (iso / ".local/state").mkdir(parents=True)
    (iso / ".cache").mkdir(parents=True)
    (iso / ".local/share").mkdir(parents=True)
    (iso / ".config/herdr/config.toml").write_text(
        "onboarding = false\n[experimental]\nallow_nested = true\n"
        "[ui.sidebar.agents]\nrows = [[\"state_icon\", { token = \"workspace\", fg = \"#89b4fa\" }, \"tab\"], [\"agent\"]]\n"
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
    subprocess.Popen(["herdr", "--session", "hseh-accept", "server"], env=base, stdout=open(slog, "w"), stderr=subprocess.STDOUT)
    time.sleep(2)
    sock = iso / ".config/herdr/sessions/hseh-accept/herdr.sock"
    env = dict(base)
    env["HERDR_SOCKET_PATH"] = str(sock)
    env["HERDR_SESSION"] = "hseh-accept"

    def herdr(*args):
        return run(env, ["herdr", *args])

    created = herdr("workspace", "create", "--label", "alpha", "--cwd", "/tmp", "--no-focus")
    herdr("workspace", "create", "--label", "beta", "--cwd", "/tmp", "--no-focus")
    pane = json.loads(created.stdout)["result"]["root_pane"]["pane_id"]
    herdr("pane", "run", pane, "sh -c 'while true; do date +HSEH_TICK_%s; sleep 1; done'")
    herdr("plugin", "link", str(HSEH.parent))
    time.sleep(1.2)
    before_focus = herdr("workspace", "list").stdout
    (EVIDENCE / "integrated-before-focus.json").write_text(before_focus)

    pid, fd = pty.fork()
    if pid == 0:
        os.environ.clear()
        os.environ.update(env)
        fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        os.execvp("herdr", ["herdr", "--session", "hseh-accept"])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
    chunks = []
    drain(fd, 2.0, chunks)
    invoke = herdr("plugin", "action", "invoke", "hseh.spaces")
    (EVIDENCE / "integrated-action-invoke.txt").write_text(invoke.stdout + invoke.stderr)
    drain(fd, 2.5, chunks)
    for key in (b"\x1b[B", b"\x1b[B"):
        os.write(fd, key)
        drain(fd, 1.0, chunks)
    wide = b"".join(chunks)
    (EVIDENCE / "integrated-herdr-wide.raw").write_bytes(wide)
    os.write(fd, b"\x1b")
    drain(fd, 1.0, chunks)
    after_esc = herdr("workspace", "list").stdout
    (EVIDENCE / "integrated-after-escape.json").write_text(after_esc)
    invoke2 = herdr("plugin", "action", "invoke", "hseh.spaces")
    drain(fd, 2.0, chunks)
    os.write(fd, b"\r")
    drain(fd, 1.5, chunks)
    after_enter = herdr("workspace", "list").stdout
    (EVIDENCE / "integrated-after-enter.json").write_text(after_enter)
    os.close(fd)
    try:
        os.waitpid(pid, 0)
    except ChildProcessError:
        pass
    herdr("server", "stop")
    time.sleep(1)
    after = sha256(USER_PLUGINS)
    (EVIDENCE / "integrated-notes.txt").write_text(
        f"iso={iso}\nbefore={before}\nafter={after}\nwide_len={len(wide)}\n"
        f"has_hseh={b'hseh' in wide}\nhas_tick={b'HSEH_TICK' in wide}\n"
        f"action={invoke.stdout}\n"
    )
    shutil.rmtree(iso, ignore_errors=True)
    print("user_plugins_unchanged", before == after)
    print("wide_len", len(wide), "has_hseh", b"hseh" in wide, "has_tick", b"HSEH_TICK" in wide)


if __name__ == "__main__":
    main()
