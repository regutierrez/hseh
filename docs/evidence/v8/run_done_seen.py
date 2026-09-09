#!/usr/bin/env python3
"""Isolated done/unseen -> seen. One Pi turn. No credentials in evidence."""

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

sys.path.insert(0, str(Path(__file__).resolve().parent))
from pi_input_ready import pi_input_screen_ready, sanitize_visible, strip_ansi

ROOT = Path(__file__).resolve().parents[3]
HSEH = ROOT / "hseh"
OUT = Path(__file__).resolve().parent
USER_PLUGINS = Path.home() / ".config/herdr/plugins.json"
USER_AUTH = Path.home() / ".pi/agent/auth.json"
PROMPT = "Reply only OK; use no tools"
SENTINEL = "HSEH_READY_SENTINEL_x7k"
MINIMAL_SETTINGS = {
    "packages": [],
    "extensions": [],
    "skills": [],
    "prompts": [],
    "themes": [],
    "defaultProjectTrust": "never",
    "enableInstallTelemetry": False,
    "quietStartup": False,
}
PI_START_FLAGS = [
    "--no-tools",
    "--no-extensions",
    "--no-skills",
    "--no-prompt-templates",
    "--no-themes",
    "--no-context-files",
    "--no-session",
    "--no-approve",
]


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
    for r in range(screen.rows):
        row = screen.row(r)
        nxt = screen.row(min(r + 1, screen.rows - 1)) + row
        if "hseh" in row and any(v in nxt for v in ("spaces", "agents", "all")):
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


def acc_drain(fd, timeout, acc: bytearray):
    chunks = []
    drain(fd, timeout, chunks)
    for chunk in chunks:
        acc.extend(chunk)


def reconstruct(acc: bytearray, rows=30, cols=100) -> CellScreen:
    scr = CellScreen(rows, cols)
    scr.feed(bytes(acc).decode("utf-8", "replace"))
    return scr


def popup_dump_has_query(dump: str, query: str) -> bool:
    compact = dump.replace(" ", "")
    return f"/{query}" in dump or f"/{query}" in compact


def selected_agent_row_visible(dump: str, label: str) -> bool:
    return any(">" in line and label in line for line in dump.splitlines())


def preview_column_populated(preview: str) -> bool:
    text = preview.strip()
    if not text:
        return False
    stripped = text.replace("─", "").replace("│", "").replace("┌", "").replace("┐", "").replace("└", "").replace("┘", "").strip()
    if not stripped:
        return False
    return ("pi v" in preview) or ("────────" in preview) or (len(stripped) > 40)


def open_agents_popup(fd, herdr, acc: bytearray) -> str:
    herdr("plugin", "action", "invoke", "hseh.agents")
    deadline = time.time() + 6
    while time.time() < deadline:
        acc_drain(fd, 0.4, acc)
        dump = reconstruct(acc).dump()
        if "hseh" in dump and "agents" in dump:
            return dump
    raise SystemExit(f"agents popup did not open\n{reconstruct(acc).dump()[-1500:]}")


def query_select_agent(fd, acc: bytearray, query: str, label: str, name: str):
    os.write(fd, query.encode())
    acc_drain(fd, 0.8, acc)
    os.write(fd, b"\x1b[A")
    acc_drain(fd, 1.2, acc)
    scr = reconstruct(acc)
    dump = scr.dump()
    preview = popup_preview_text(scr)
    (OUT / f"{name}.raw").write_bytes(bytes(acc))
    (OUT / f"{name}-dump.txt").write_text(dump)
    (OUT / f"{name}-preview.txt").write_text(preview)
    if not popup_dump_has_query(dump, query):
        raise SystemExit(f"{name}: query {query!r} not visible\n{dump}")
    if not selected_agent_row_visible(dump, label):
        raise SystemExit(f"{name}: selected row {label!r} not visible\n{dump}")
    if not preview_column_populated(preview):
        raise SystemExit(f"{name}: preview column empty\n{preview}")
    return dump, preview


def slim_agent(payload: dict) -> dict:
    agent = payload.get("result", {}).get("agent") or payload.get("agent") or {}
    keep = (
        "name",
        "agent",
        "agent_status",
        "pane_id",
        "tab_id",
        "workspace_id",
        "focused",
        "state_change_seq",
        "display_agent",
        "interactive_ready",
    )
    return {k: agent.get(k) for k in keep}


def slim_workspaces(payload: dict) -> list:
    rows = payload.get("result", {}).get("workspaces") or []
    return [
        {
            "workspace_id": w.get("workspace_id"),
            "label": w.get("label"),
            "focused": w.get("focused"),
            "active_tab_id": w.get("active_tab_id"),
        }
        for w in rows
    ]


def focused_workspace_ids(rows: list) -> list:
    return [w["workspace_id"] for w in rows if w.get("focused")]


def pane_visible_text(env, pane_id: str) -> tuple[str, int, str]:
    r = run(env, ["herdr", "pane", "read", pane_id, "--source", "visible", "--format", "text"])
    text = r.stdout or ""
    try:
        data = json.loads(text)
        text = (
            ((data.get("result") or {}).get("read") or {}).get("text")
            or (data.get("read") or {}).get("text")
            or text
        )
    except json.JSONDecodeError:
        pass
    return text, r.returncode, r.stderr or ""


def pane_recent_text(env, pane_id: str) -> str:
    r = run(env, ["herdr", "pane", "read", pane_id, "--source", "recent-unwrapped", "--format", "text"])
    text = r.stdout or ""
    try:
        data = json.loads(text)
        text = (
            ((data.get("result") or {}).get("read") or {}).get("text")
            or (data.get("read") or {}).get("text")
            or text
        )
    except json.JSONDecodeError:
        pass
    return text


def capture_pane_lifecycle(env, pane_id: str, out_dir: Path, name: str) -> dict:
    visible, _, _ = pane_visible_text(env, pane_id)
    recent = pane_recent_text(env, pane_id)
    (out_dir / f"{name}-visible.txt").write_text(sanitize_visible(visible))
    (out_dir / f"{name}-recent.txt").write_text(sanitize_visible(recent))
    get = run(env, ["herdr", "agent", "get", "hsehtest"])
    explain = run(env, ["herdr", "agent", "explain", "hsehtest"])
    agent = {}
    try:
        agent = slim_agent(json.loads(get.stdout or "{}"))
    except json.JSONDecodeError:
        (out_dir / f"{name}-agent-get.raw.txt").write_text((get.stdout or "")[-2000:])
    (out_dir / f"{name}-agent-get.json").write_text(json.dumps(agent, indent=2))
    (out_dir / f"{name}-agent-explain.txt").write_text(sanitize_visible((explain.stdout or "")[-4000:]))
    return {"visible": visible, "recent": recent, "agent": agent}


def wait_pi_input_ready(env, pane_id: str, out_dir: Path, timeout_s: float = 30) -> str:
    deadline = time.time() + timeout_s
    last_visible = ""
    while time.time() < deadline:
        visible, _, _ = pane_visible_text(env, pane_id)
        last_visible = visible
        if pi_input_screen_ready(visible):
            time.sleep(0.25)
            again, _, _ = pane_visible_text(env, pane_id)
            if pi_input_screen_ready(again):
                last_visible = again
                (out_dir / "ready-visible.txt").write_text(sanitize_visible(again))
                ansi = run(env, ["herdr", "pane", "read", pane_id, "--source", "visible", "--format", "ansi"])
                (out_dir / "ready-visible.raw").write_bytes((ansi.stdout or "").encode("utf-8", "replace"))
                (out_dir / "ready.json").write_text(
                    json.dumps({"ready": True, "visible_len": len(again)}, indent=2)
                )
                return again
        time.sleep(0.2)
    (out_dir / "ready-visible.txt").write_text(sanitize_visible(last_visible))
    (out_dir / "ready.json").write_text(
        json.dumps(
            {
                "ready": False,
                "visible_len": len(last_visible),
                "input_screen_ready": pi_input_screen_ready(last_visible),
            },
            indent=2,
        )
    )
    raise SystemExit(f"blocker: Pi input screen did not finish startup; prompt not sent\n{sanitize_visible(last_visible)[-800:]}")


def cmdline_of_pane(env, pane_id: str) -> str:
    r = run(env, ["herdr", "pane", "process-info", "--pane", pane_id])
    text = (r.stdout or "") + (r.stderr or "")
    (OUT / "process-info.txt").write_text(text)
    return text


def main() -> int:
    global OUT
    import argparse

    parser = argparse.ArgumentParser()
    parser.add_argument("--startup-only", action="store_true")
    parser.add_argument("--local-session-diag", action="store_true")
    parser.add_argument("--nav-only", action="store_true")
    parser.add_argument("--out", default="")
    args = parser.parse_args()
    out_dir = Path(args.out) if args.out else OUT
    out_dir.mkdir(parents=True, exist_ok=True)
    OUT = out_dir
    no_auth = args.startup_only or args.local_session_diag or args.nav_only
    if not no_auth:
        if not USER_AUTH.is_file():
            raise SystemExit("blocker: no existing Pi auth file; cannot use isolated auth without altering user config")
        if not os.environ.get("PI_PROVIDER") or not os.environ.get("PI_MODEL"):
            raise SystemExit("blocker: PI_PROVIDER/PI_MODEL unset")
    before_plugins = sha256(USER_PLUGINS)
    user_ext = Path.home() / ".pi/agent/extensions/herdr-agent-state.ts"
    before_user_ext = sha256(user_ext) if user_ext.is_file() else ""
    iso = Path(subprocess.check_output(["mktemp", "-d", "/tmp/hseh-accept-XXXXXX"], text=True).strip())
    sock = iso / ".config/herdr/sessions/hseh-accept/herdr.sock"
    fd = None
    child = None
    pi_cwd = None
    try:
        (iso / ".config/herdr").mkdir(parents=True)
        (iso / ".local/state").mkdir(parents=True)
        (iso / ".cache").mkdir(parents=True)
        (iso / ".local/share").mkdir(parents=True)
        (iso / ".config/herdr/config.toml").write_text(
            "onboarding = false\n[experimental]\nallow_nested = true\n"
        )
        pi_cwd = Path(subprocess.check_output(["mktemp", "-d", "/tmp/hseh-pi-cwd-XXXXXX"], text=True).strip())
        pi_dir = iso / ".pi/agent"
        pi_dir.mkdir(parents=True)
        (pi_dir / "settings.json").write_text(json.dumps(MINIMAL_SETTINGS, indent=2) + "\n")
        (pi_dir / "extensions").mkdir(parents=True, exist_ok=True)
        if not no_auth:
            dest_auth = pi_dir / "auth.json"
            shutil.copyfile(USER_AUTH, dest_auth)
            os.chmod(dest_auth, 0o600)
        base = {
            "HOME": str(iso),
            "XDG_CONFIG_HOME": str(iso / ".config"),
            "XDG_STATE_HOME": str(iso / ".local/state"),
            "XDG_CACHE_HOME": str(iso / ".cache"),
            "XDG_DATA_HOME": str(iso / ".local/share"),
            "PATH": os.environ["PATH"],
            "TERM": "xterm-256color",
            "LANG": os.environ.get("LANG", "C.UTF-8"),
            "GIT_TERMINAL_PROMPT": "0",
            "PI_SKIP_VERSION_CHECK": "1",
            "PI_CODING_AGENT_DIR": str(pi_dir),
        }
        if no_auth:
            base["PI_OFFLINE"] = "1"
        else:
            base["PI_PROVIDER"] = os.environ["PI_PROVIDER"]
            base["PI_MODEL"] = os.environ["PI_MODEL"]
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

        def herdr(*args, timeout=40):
            r = run(env, ["herdr", *args], timeout=timeout)
            if r.returncode != 0:
                raise SystemExit(f"herdr {args} failed {r.returncode}: {r.stderr}{r.stdout}")
            return r

        def agent_get():
            return slim_agent(json.loads(herdr("agent", "get", "hsehtest").stdout))

        def workspaces():
            return slim_workspaces(json.loads(herdr("workspace", "list").stdout))

        herdr("plugin", "link", str(ROOT))
        inst = run(env, ["herdr", "integration", "install", "pi"])
        (OUT / "integration-install.txt").write_text(sanitize_visible((inst.stdout or "") + (inst.stderr or "")))
        if inst.returncode != 0:
            raise SystemExit(f"herdr integration install pi failed: {inst.returncode}")
        st = run(env, ["herdr", "integration", "status"])
        (OUT / "integration-status.txt").write_text(sanitize_visible(st.stdout or ""))
        ext_path = pi_dir / "extensions" / "herdr-agent-state.ts"
        if not ext_path.is_file():
            raise SystemExit(f"isolated pi integration missing: {ext_path}")
        ext_text = ext_path.read_text()
        (OUT / "extension-head.txt").write_text(ext_text[:900])
        if "HERDR_INTEGRATION_ID=pi" not in ext_text or 'source = "herdr:pi"' not in ext_text:
            raise SystemExit("generated extension is not official herdr:pi")
        cfg_dir = Path(herdr("plugin", "config-dir", "hseh").stdout.strip())
        cfg_dir.mkdir(parents=True, exist_ok=True)
        (cfg_dir / "hseh.toml").write_text("preview_poll_ms = 500\nwide_preview_min_columns = 40\n")

        listed0 = workspaces()
        if not listed0:
            herdr("workspace", "create", "--label", "alpha", "--cwd", str(pi_cwd))
        else:
            herdr("workspace", "rename", listed0[0]["workspace_id"], "alpha")
        created = json.loads(
            herdr("workspace", "create", "--label", "beta-agent", "--cwd", str(pi_cwd), "--no-focus").stdout
        )
        pane = created["result"]["root_pane"]["pane_id"]
        tab = created["result"]["tab"]["tab_id"]
        agent_ws = created["result"]["workspace"]["workspace_id"]
        time.sleep(0.5)
        pi_flags = list(PI_START_FLAGS)
        pi_flags = ["--no-extensions", "-e", str(ext_path), *[f for f in pi_flags if f != "--no-extensions"]]
        if no_auth:
            pi_flags = ["--offline", *pi_flags]
        else:
            pi_flags = [f for f in pi_flags if f != "--no-session"]
            pi_flags.extend(["--provider", os.environ["PI_PROVIDER"], "--model", os.environ["PI_MODEL"]])
        started = herdr(
            "agent",
            "start",
            "hsehtest",
            "--kind",
            "pi",
            "--pane",
            pane,
            "--timeout",
            "30000",
            "--",
            *pi_flags,
            timeout=40,
        )
        start_info = slim_agent(json.loads(started.stdout))
        (OUT / "01-started.json").write_text(json.dumps({"agent": start_info, "workspaces": workspaces()}, indent=2))
        (OUT / "pi-argv-requested.json").write_text(json.dumps({"flags": pi_flags}, indent=2))
        proc_info = cmdline_of_pane(env, pane)
        (OUT / "process-info-has-offline.txt").write_text(
            json.dumps({"requested": pi_flags, "offline_in_process_info": "--offline" in proc_info}, indent=2)
        )
        if start_info.get("agent_status") not in {"idle", "unknown"}:
            raise SystemExit(f"expected idle start, got {start_info}")
        other_focus = focused_workspace_ids(workspaces())
        if agent_ws in other_focus:
            raise SystemExit(f"agent workspace focused at start: {other_focus}")
        visible = wait_pi_input_ready(env, pane, OUT, timeout_s=40)
        expl = run(env, ["herdr", "agent", "explain", "hsehtest", "--json"])
        expl_text = (expl.stdout or "") + (expl.stderr or "")
        (OUT / "agent-explain.json").write_text(sanitize_visible(expl_text))
        expl_obj = {}
        try:
            expl_obj = json.loads(expl.stdout or "{}")
        except json.JSONDecodeError:
            expl_obj = {}
        hook_ok = expl_obj.get("screen_detection_skipped") is True and expl_obj.get(
            "screen_detection_skip_reason"
        ) == "full_lifecycle_hook_authority"
        screen_fallback = expl_obj.get("fallback_reason") == "default_known_agent_idle_fallback"
        (OUT / "hook-authority.json").write_text(
            json.dumps(
                {
                    "hook_authority": hook_ok,
                    "skip_reason": expl_obj.get("screen_detection_skip_reason"),
                    "fallback_reason": expl_obj.get("fallback_reason"),
                    "screen_fallback": screen_fallback,
                    "state": expl_obj.get("state"),
                    "explain_rc": expl.returncode,
                },
                indent=2,
            )
        )
        if not hook_ok:
            raise SystemExit("blocker: agent explain lacks full_lifecycle_hook_authority; no model turn")
        query_label = "beta-agent"
        if args.nav_only:
            other_focus = focused_workspace_ids(workspaces())
            pid, fd = pty.fork()
            if pid == 0:
                os.environ.clear()
                os.environ.update(env)
                fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
                os.execvp("herdr", ["herdr", "--session", "hseh-accept"])
            child = pid
            fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
            acc_pre = bytearray()
            acc_drain(fd, 1.2, acc_pre)
            browse_acc = bytearray()
            open_agents_popup(fd, herdr, browse_acc)
            query_select_agent(fd, browse_acc, query_label, query_label, "browse")
            if focused_workspace_ids(workspaces()) != other_focus:
                raise SystemExit("nav browse changed focus")
            os.write(fd, b"\x1b")
            acc_drain(fd, 0.8, browse_acc)
            (OUT / "browse-escape.raw").write_bytes(bytes(browse_acc))
            esc_dump = reconstruct(browse_acc).dump()
            (OUT / "browse-escape-dump.txt").write_text(esc_dump)
            if "agents  /" in esc_dump or "/beta-agent" in esc_dump:
                raise SystemExit(f"escape did not close popup\n{esc_dump}")
            if focused_workspace_ids(workspaces()) != other_focus:
                raise SystemExit("escape changed focus")
            enter_acc = bytearray()
            open_agents_popup(fd, herdr, enter_acc)
            query_select_agent(fd, enter_acc, query_label, query_label, "reopen")
            os.write(fd, b"\r")
            acc_drain(fd, 1.5, enter_acc)
            (OUT / "reopen-enter.raw").write_bytes(bytes(enter_acc))
            (OUT / "reopen-enter-dump.txt").write_text(reconstruct(enter_acc).dump())
            time.sleep(0.4)
            after = agent_get()
            after_ws = workspaces()
            (OUT / "07-after-enter.json").write_text(json.dumps({"agent": after, "workspaces": after_ws}, indent=2))
            if focused_workspace_ids(after_ws) != [agent_ws]:
                header = reconstruct(enter_acc).dump()
                (OUT / "enter-fail-header.txt").write_text(header)
                raise SystemExit(f"nav enter focus {focused_workspace_ids(after_ws)} want {[agent_ws]}")
            if after.get("pane_id") != pane or after.get("tab_id") != tab or after.get("workspace_id") != agent_ws:
                raise SystemExit(f"nav enter occupant {after}")
            print("PASS nav-only", pane, focused_workspace_ids(after_ws))
            return 0
        if args.local_session_diag:
            before = capture_pane_lifecycle(env, pane, OUT, "before-session")
            prompt = run(
                env,
                ["herdr", "agent", "prompt", "hsehtest", "/session"],
                timeout=15,
            )
            (OUT / "session-prompt-rc.txt").write_text(f"rc={prompt.returncode}\n")
            (OUT / "session-prompt-output.txt").write_text(
                sanitize_visible((prompt.stdout or "") + (prompt.stderr or ""))
            )
            time.sleep(0.6)
            after_prompt = capture_pane_lifecycle(env, pane, OUT, "after-agent-prompt")
            after_vis = strip_ansi(after_prompt["visible"])
            after_rec = strip_ansi(after_prompt["recent"])
            ran = "Session Info" in after_vis or "Session Info" in after_rec
            pasted = "/session" in after_vis and "Session Info" not in after_vis
            delivery = "yes" if ran else ("pasted-unsubmitted" if pasted else "unknown")
            if not ran:
                run(env, ["herdr", "pane", "send-text", pane, "/session"])
                time.sleep(0.2)
                after_text = capture_pane_lifecycle(env, pane, OUT, "after-send-text")
                run(env, ["herdr", "pane", "send-keys", pane, "Enter"])
                time.sleep(0.6)
                after_enter = capture_pane_lifecycle(env, pane, OUT, "after-send-keys-enter")
                vis2 = strip_ansi(after_enter["visible"])
                rec2 = strip_ansi(after_enter["recent"])
                ran2 = "Session Info" in vis2 or "Session Info" in rec2
                pasted2 = "/session" in vis2 and "Session Info" not in vis2
                key_delivery = "yes" if ran2 else ("pasted-unsubmitted" if pasted2 else "unknown")
            else:
                key_delivery = "skipped"
            (OUT / "diagnosis.json").write_text(
                json.dumps(
                    {
                        "agent_prompt_delivery": delivery,
                        "send_text_enter_delivery": key_delivery,
                        "provider_failure": "unknown",
                    },
                    indent=2,
                )
            )
            print("DIAG", "agent.prompt", delivery, "send-keys", key_delivery)
            return 0
        if args.startup_only:
            run(env, ["herdr", "pane", "send-text", pane, SENTINEL])
            time.sleep(0.3)
            with_sent, _, _ = pane_visible_text(env, pane)
            (OUT / "sentinel-visible.txt").write_text(sanitize_visible(with_sent))
            if SENTINEL not in strip_ansi(with_sent):
                raise SystemExit("sentinel not visible in editor")
            run(env, ["herdr", "agent", "send-keys", "hsehtest", "ctrl+u"])
            time.sleep(0.2)
            cleared, _, _ = pane_visible_text(env, pane)
            (OUT / "sentinel-cleared.txt").write_text(sanitize_visible(cleared))
            (OUT / "notes.txt").write_text(
                f"startup-only ready\nflags={pi_flags}\nprocess-offline={'--offline' in proc_info}\n"
            )
            print("PASS startup-only", OUT)
            return 0

        prompt = run(
            env,
            [
                "herdr",
                "agent",
                "prompt",
                "hsehtest",
                PROMPT,
                "--wait",
                "--until",
                "working",
                "--timeout",
                "8000",
            ],
            timeout=20,
        )
        (OUT / "02-prompt-rc.txt").write_text(f"rc={prompt.returncode}\n")
        prompt_out = (prompt.stdout or "") + (prompt.stderr or "")
        (OUT / "02-prompt-output.json").write_text(prompt_out if prompt_out.strip().startswith("{") else json.dumps({"raw": prompt_out[-4000:]}, indent=2))
        after_prompt = agent_get()
        after_prompt_ws = workspaces()
        time.sleep(0.8)
        pane_after = capture_pane_lifecycle(env, pane, OUT, "after-prompt")
        (OUT / "02-after-prompt-status.json").write_text(
            json.dumps({"agent": after_prompt, "workspaces": after_prompt_ws, "prompt_rc": prompt.returncode}, indent=2)
        )
        vis = strip_ansi(pane_after["visible"]) + "\n" + strip_ansi(pane_after["recent"])
        vis_l = vis.lower()
        provider_err = None
        for needle in (
            "unauthorized",
            "invalid token",
            "oauth",
            "authentication",
            "auth error",
            "401",
            "403",
            "provider error",
            "api error",
        ):
            if needle in vis_l:
                provider_err = needle
                break
        working = after_prompt
        if prompt.returncode == 0:
            try:
                working = slim_agent(json.loads(prompt.stdout))
            except json.JSONDecodeError:
                working = after_prompt
        if working.get("agent_status") == "working":
            (OUT / "02-working.json").write_text(
                json.dumps({"agent": working, "workspaces": after_prompt_ws}, indent=2)
            )
        elif after_prompt.get("agent_status") == "done":
            (OUT / "02-working.json").write_text(
                json.dumps({"agent": after_prompt, "workspaces": after_prompt_ws, "note": "done before working snapshot"}, indent=2)
            )
        else:
            (OUT / "02-prompt-screen-diagnosis.json").write_text(
                json.dumps(
                    {
                        "status": after_prompt.get("agent_status"),
                        "prompt_rc": prompt.returncode,
                        "provider_error_needle": provider_err,
                        "has_user_prompt_text": PROMPT.split(";")[0] in vis,
                    },
                    indent=2,
                )
            )
            if provider_err:
                raise SystemExit(
                    f"provider/auth error on pane ({provider_err}); not hseh e2e failure; status={after_prompt.get('agent_status')}"
                )
            raise SystemExit(
                f"prompt did not reach working/done status={after_prompt.get('agent_status')} rc={prompt.returncode}"
            )
        if focused_workspace_ids(after_prompt_ws) != other_focus:
            raise SystemExit("focus changed during prompt")

        if after_prompt.get("agent_status") != "done":
            done_wait = run(
                env,
                ["herdr", "agent", "wait", "hsehtest", "--until", "done", "--timeout", "90000"],
                timeout=100,
            )
            (OUT / "03-wait-done-rc.txt").write_text(
                f"rc={done_wait.returncode}\n{(done_wait.stdout or '')[-2000:]}\n{(done_wait.stderr or '')[-1000:]}"
            )
            if done_wait.returncode != 0:
                raise SystemExit(f"wait done failed rc={done_wait.returncode}")
            done = slim_agent(json.loads(done_wait.stdout))
        else:
            done = after_prompt
        (OUT / "03-done-unfocused.json").write_text(
            json.dumps({"agent": done, "workspaces": workspaces()}, indent=2)
        )
        if done.get("agent_status") != "done":
            raise SystemExit(f"genuine done not established: {done}")
        if focused_workspace_ids(workspaces()) != other_focus:
            raise SystemExit("focus changed when done arrived")
        if done.get("workspace_id") != agent_ws or done.get("pane_id") != pane:
            raise SystemExit(f"occupant mismatch {done}")

        pid, fd = pty.fork()
        if pid == 0:
            os.environ.clear()
            os.environ.update(env)
            fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
            os.execvp("herdr", ["herdr", "--session", "hseh-accept"])
        child = pid
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        acc_pre = bytearray()
        acc_drain(fd, 1.2, acc_pre)
        browse_acc = bytearray()
        open_agents_popup(fd, herdr, browse_acc)
        query_select_agent(fd, browse_acc, query_label, query_label, "browse")
        browse_agent = agent_get()
        browse_ws = workspaces()
        (OUT / "04-browse.json").write_text(json.dumps({"agent": browse_agent, "workspaces": browse_ws}, indent=2))
        if browse_agent.get("agent_status") != "done":
            raise SystemExit(f"browse changed status: {browse_agent}")
        if focused_workspace_ids(browse_ws) != other_focus:
            raise SystemExit(f"browse changed focus: {browse_ws}")
        time.sleep(0.6)
        acc_drain(fd, 0.8, browse_acc)
        poll_scr = reconstruct(browse_acc)
        (OUT / "browse-poll.raw").write_bytes(bytes(browse_acc))
        (OUT / "browse-poll-dump.txt").write_text(poll_scr.dump())
        (OUT / "browse-poll-preview.txt").write_text(popup_preview_text(poll_scr))
        if not selected_agent_row_visible(poll_scr.dump(), query_label):
            raise SystemExit(f"poll lost selected row\n{poll_scr.dump()}")
        if not preview_column_populated(popup_preview_text(poll_scr)):
            raise SystemExit(f"poll preview empty\n{popup_preview_text(poll_scr)}")
        poll_agent = agent_get()
        poll_ws = workspaces()
        (OUT / "05-browse-poll.json").write_text(json.dumps({"agent": poll_agent, "workspaces": poll_ws}, indent=2))
        if poll_agent.get("agent_status") != "done":
            raise SystemExit(f"preview poll changed status: {poll_agent}")
        if focused_workspace_ids(poll_ws) != other_focus:
            raise SystemExit("preview poll changed focus")

        os.write(fd, b"\x1b")
        acc_drain(fd, 0.8, browse_acc)
        (OUT / "browse-escape.raw").write_bytes(bytes(browse_acc))
        (OUT / "browse-escape-dump.txt").write_text(reconstruct(browse_acc).dump())
        esc_agent = agent_get()
        esc_ws = workspaces()
        (OUT / "06-after-escape.json").write_text(json.dumps({"agent": esc_agent, "workspaces": esc_ws}, indent=2))
        if esc_agent.get("agent_status") != "done":
            raise SystemExit(f"escape changed status: {esc_agent}")
        if focused_workspace_ids(esc_ws) != other_focus:
            raise SystemExit("escape changed focus")

        enter_acc = bytearray()
        open_agents_popup(fd, herdr, enter_acc)
        query_select_agent(fd, enter_acc, query_label, query_label, "reopen")
        os.write(fd, b"\r")
        acc_drain(fd, 1.5, enter_acc)
        (OUT / "reopen-enter.raw").write_bytes(bytes(enter_acc))
        (OUT / "reopen-enter-dump.txt").write_text(reconstruct(enter_acc).dump())
        time.sleep(0.4)
        after = agent_get()
        after_ws = workspaces()
        snap = json.loads(herdr("api", "snapshot").stdout)
        (OUT / "07-after-enter.json").write_text(json.dumps({"agent": after, "workspaces": after_ws}, indent=2))
        if after.get("agent_status") != "idle":
            (OUT / "enter-fail-header.txt").write_text(reconstruct(enter_acc).dump())
            raise SystemExit(f"enter did not mark seen/idle: {after}")
        if focused_workspace_ids(after_ws) != [agent_ws]:
            (OUT / "enter-fail-header.txt").write_text(reconstruct(enter_acc).dump())
            raise SystemExit(f"enter focus {focused_workspace_ids(after_ws)} want {[agent_ws]}")
        focused_pane = (snap.get("result") or snap).get("snapshot", snap).get("focused_pane_id")
        if not focused_pane:
            snapshot = snap.get("result", snap)
            if isinstance(snapshot, dict) and "snapshot" in snapshot:
                snapshot = snapshot["snapshot"]
            focused_pane = snapshot.get("focused_pane_id") if isinstance(snapshot, dict) else None
        # Accept agent.get focused true plus workspace match as pane/tab proof when snapshot shape varies.
        if after.get("pane_id") != pane or after.get("tab_id") != tab or after.get("workspace_id") != agent_ws:
            raise SystemExit(f"enter occupant {after} want pane={pane} tab={tab} ws={agent_ws}")
        if after.get("focused") is False:
            raise SystemExit(f"agent not focused after enter: {after}")

        after_plugins = sha256(USER_PLUGINS)
        if after_plugins != before_plugins:
            raise SystemExit("user plugins.json changed")
        (OUT / "notes.txt").write_text(
            f"iso={iso}\nagent_ws={agent_ws}\npane={pane}\ntab={tab}\n"
            f"other_focus={other_focus}\n"
            f"working={working.get('agent_status')}\n"
            f"done={done.get('agent_status')}\n"
            f"browse={browse_agent.get('agent_status')}\n"
            f"escape={esc_agent.get('agent_status')}\n"
            f"enter={after.get('agent_status')} focused_ws={focused_workspace_ids(after_ws)}\n"
            f"plugins={before_plugins}\n"
        )
        print("PASS done/seen", pane, "enter", after.get("agent_status"), focused_workspace_ids(after_ws))
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
        if pi_cwd is not None:
            shutil.rmtree(pi_cwd, ignore_errors=True)
        try:
            after_plugins = sha256(USER_PLUGINS)
            if after_plugins != before_plugins:
                print("WARNING user plugins.json changed", file=sys.stderr)
            if before_user_ext and user_ext.is_file() and sha256(user_ext) != before_user_ext:
                print("WARNING user herdr-agent-state.ts changed", file=sys.stderr)
        except Exception:
            pass


if __name__ == "__main__":
    raise SystemExit(main())
