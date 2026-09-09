#!/usr/bin/env python3
"""Start an idle Pi in an isolated Herdr session. No prompts."""

import json
import os
import shutil
import subprocess
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
HSEH = ROOT / "hseh"
OUT = Path(__file__).resolve().parent


def run(env, args, timeout=40):
    return subprocess.run(args, env=env, capture_output=True, text=True, timeout=timeout)


def main() -> int:
    iso = Path(subprocess.check_output(["mktemp", "-d", "/tmp/hseh-agent-XXXXXX"], text=True).strip())
    sock = iso / ".config/herdr/sessions/hseh-accept/herdr.sock"
    try:
        (iso / ".config/herdr").mkdir(parents=True)
        (iso / ".local/state").mkdir(parents=True)
        (iso / ".cache").mkdir(parents=True)
        (iso / ".local/share").mkdir(parents=True)
        (iso / ".config/herdr/config.toml").write_text("onboarding = false\n[experimental]\nallow_nested = true\n")
        base = {
            "HOME": str(iso),
            "XDG_CONFIG_HOME": str(iso / ".config"),
            "XDG_STATE_HOME": str(iso / ".local/state"),
            "XDG_CACHE_HOME": str(iso / ".cache"),
            "XDG_DATA_HOME": str(iso / ".local/share"),
            "PATH": os.environ["PATH"],
            "TERM": "xterm-256color",
        }
        subprocess.Popen(["herdr", "--session", "hseh-accept", "server"], env=base, stdout=open(iso / "server.log", "w"), stderr=subprocess.STDOUT)
        for _ in range(30):
            if sock.exists():
                break
            time.sleep(0.1)
        env = dict(base)
        env["HERDR_SOCKET_PATH"] = str(sock)
        env["HERDR_SESSION"] = "hseh-accept"
        env["HERDR_PLUGIN_STATE_DIR"] = str(iso / ".local/state/herdr/plugins/hseh")
        created = run(env, ["herdr", "workspace", "create", "--label", "pi-idle", "--cwd", "/tmp", "--no-focus"])
        if created.returncode != 0:
            raise SystemExit(created.stderr + created.stdout)
        pane = json.loads(created.stdout)["result"]["root_pane"]["pane_id"]
        time.sleep(0.4)
        started = run(env, ["herdr", "agent", "start", "hsehtest", "--kind", "pi", "--pane", pane, "--timeout", "8000"])
        (OUT / "idle-pi-start.json").write_text(started.stdout + started.stderr)
        if started.returncode != 0:
            raise SystemExit(f"agent start failed: {started.stderr}{started.stdout}")
        before = json.loads(started.stdout)["result"]["agent"]["agent_status"]
        listed = run(env, [str(HSEH), "list", "--json", "--view", "agents"])
        (OUT / "idle-pi-list.json").write_text(listed.stdout)
        if listed.returncode != 0:
            raise SystemExit(listed.stderr + listed.stdout)
        doc = json.loads(listed.stdout)
        if not doc["items"] or doc["items"][0]["kind"] != "agent" or doc["items"][0]["status"] != "idle":
            raise SystemExit(f"list did not show idle agent: {doc}")
        after = run(env, ["herdr", "agent", "get", "hsehtest"])
        (OUT / "idle-pi-get-after-list.json").write_text(after.stdout)
        status = json.loads(after.stdout)["result"]["agent"]["agent_status"]
        if status != "idle":
            raise SystemExit(f"CLI list marked agent seen? status={status} before={before}")
        print("PASS idle pi", doc["items"][0]["id"], status)
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
            time.sleep(0.3)
        shutil.rmtree(iso, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())
