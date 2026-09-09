"""Positive Pi interactive editor detection. Markers from installed pi-coding-agent source."""

from __future__ import annotations

import re

# Header compact instructions: interactive-mode.js compactInstructions + APP_NAME "pi"
# Footer: footer.js contextPercentDisplay and modelName
# Editor: dynamic-border.js renders "─".repeat(width)
# Reject: project-trust.js "Trust project folder?", first-time-setup.js welcome/theme/analytics

SPINNERS = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏◐◓◑◒"
EDITOR_BORDER = "────────"
HEADER_APP = "pi v"
HEADER_INTERRUPT = "interrupt"
HEADER_COMMANDS = "commands"
HEADER_BASH = "bash"
FOOTER_AUTO = "(auto)"
FOOTER_NO_MODEL = "no-model"
TRUST_PROMPT = "Trust project folder?"
FIRST_TIME_WELCOME = "Welcome to pi, the minimal coding agent."
FIRST_TIME_THEME = "Pick a theme."
FIRST_TIME_ANALYTICS = "anonymous usage data"
SHELL_PI = "$ pi"

_ANSI_RE = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]|\x1b\].*?(?:\x07|\x1b\\)")


def strip_ansi(text: str) -> str:
    return _ANSI_RE.sub("", text or "")


def sanitize_visible(text: str) -> str:
    t = strip_ansi(text)
    t = re.sub(r"(?i)\bsk-[A-Za-z0-9_-]{8,}", "<key>", t)
    t = re.sub(r"(?i)\beyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9._-]+", "<jwt>", t)
    t = re.sub(r"(?i)(authorization|api[_-]?key|secret|bearer)([=:\s]+)(\S+)", r"\1\2<redacted>", t)
    return t


def pi_input_screen_ready(text: str) -> bool:
    raw = strip_ansi(text)
    if any(ch in raw for ch in SPINNERS):
        return False
    if TRUST_PROMPT in raw:
        return False
    if FIRST_TIME_WELCOME in raw or FIRST_TIME_THEME in raw or FIRST_TIME_ANALYTICS in raw:
        return False
    low = raw.lower()
    if "cloning into" in low or ("added " in low and "packages" in low):
        return False
    if "found 0 vulnerabilities" in low or "npm fund" in low:
        return False
    stripped = raw.strip()
    if not stripped:
        return False
    lines = [ln.strip() for ln in stripped.splitlines() if ln.strip()]
    if any(ln.endswith(SHELL_PI) or ln == "pi" for ln in lines) and EDITOR_BORDER not in raw:
        return False
    if "sh-" in stripped and SHELL_PI.strip() in stripped and EDITOR_BORDER not in raw:
        return False
    has_header = HEADER_APP in raw and HEADER_INTERRUPT in raw and HEADER_COMMANDS in raw and HEADER_BASH in raw
    has_editor = EDITOR_BORDER in raw
    has_footer = FOOTER_AUTO in raw or FOOTER_NO_MODEL in raw or "?/" in raw or "%/" in raw
    if not (has_header and has_editor and has_footer):
        return False
    if "unauthorized" in low or "sign in" in low:
        return False
    return True
