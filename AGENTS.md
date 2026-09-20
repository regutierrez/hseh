# AGENTS.md

`go test ./...` is required and uses a fake herdr socket. It does not prove the picker looks or behaves inside a real herdr session.

For UI, theme, popup, plugin, or anything a person would see: install herdr, install this checkout, run it, and keep proof from that session.

## Ephemeral environment

Cloud agents, fresh VMs, and any machine without herdr already installed:

1. Install herdr.
2. Build and link **this checkout** (not the published GitHub default).
3. Start herdr in a real terminal.
4. Exercise the change the way a person would.
5. Capture proof from that UI (screenshot or recording). A green `go test` is not that proof.

### 1. Herdr

```sh
curl -fsSL https://herdr.dev/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
herdr --version   # need >= 0.9.0
```

### 2. This checkout

`herdr plugin install regutierrez/hseh` clones GitHub `main`. That is the wrong binary when you are validating a branch.

```sh
go build -o hseh .
herdr plugin link "$PWD"
herdr plugin list    # must show hseh enabled [local:$PWD]
```

If a GitHub-managed install is already registered, unlink or uninstall it first.

### 3. Start herdr

Skip first-run chrome so you can actually test:

```toml
# ~/.config/herdr/config.toml
onboarding = false
```

Then from a real TTY (not a pipe):

```sh
herdr
```

### 4. Open hseh

With the server running:

```sh
herdr plugin action invoke hseh.spaces
```

Agents is `hseh.agents`. Inside herdr, Settings is `prefix+s` (default prefix `ctrl+b`).

### 5. Proof

Keep a screenshot or screen recording of herdr with hseh open that shows the change. If the change is theme-following, change herdr's theme in Settings, then reopen hseh and show the picker matching.

Listing/icon work also needs `eza` and a Nerd Font in that terminal; without them you are not testing what a person sees.
