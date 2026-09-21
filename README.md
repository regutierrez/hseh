# hseh

> i slopped this one up entirely. you have been warned.

i miss [sesh](https://github.com/joshmedeski/sesh) so i made a shittier one for herdr.

standalone linux/macos herdr space/agent picker. fuzzy search, live previews, git branch/status, reusable spaces. only spaces and agents.

needs go 1.27.1, herdr >=0.9.0, nerd fonts >=3.5 for claude/openai icons.

```sh
curl -fsSL https://herdr.dev/install.sh | sh
herdr plugin install regutierrez/hseh
```

`plugin install` clones the repo, runs `go build -o hseh .`, and registers the plugin. go must already be on PATH; there is no binary-download fallback.

local checkout:

```sh
git clone https://github.com/regutierrez/hseh.git
cd hseh
go build -o hseh .
herdr plugin link "$PWD"
```

then `herdr plugin action invoke hseh.spaces`

enable the commit hooks once per clone:

```sh
git config core.hooksPath .githooks
```

`.githooks/commit-msg` and `.githooks/prepare-commit-msg` strip Cursor attribution trailers and rewrite a Cursor Agent / `cursoragent@cursor.com` author or committer to `regutierrez <rpegutierrez@gmail.com>`.

tab changes view. enter switches. esc cancels.

## layout

single binary, so `main.go` stays at the module root (that is what
`herdr-plugin.toml` builds) and everything else lives under `internal/` so
nothing outside this module can import it:

| package | what it owns |
| --- | --- |
| `internal/config` | plugin paths (state, config, spaces dirs), `hseh.toml`, env-derived ids |
| `internal/lockfile` | flock helper shared by history, definitions and space open |
| `internal/trace` | `HSEH_TRACE` timing hooks |
| `internal/termtext` | strip terminal controls, keep only SGR |
| `internal/gitinfo` | `git status --porcelain=v2` summary and checkout root per directory |
| `internal/dirlist` | directory listing for the spaces preview: `eza` when installed, builtin fallback |
| `internal/herdr` | socket client, `session.snapshot` types, continuity witness, plugin events, workspace/tab/pane calls |
| `internal/focus` | mru focus history on disk and the plugin event hook that maintains it |
| `internal/space` | reusable space definitions, associations, open and recover |
| `internal/picker` | bubble tea ui: items, rendering, theme, sidebar, definition rows |
| `internal/hsehtest` | fake herdr socket server and fixtures for tests |

`commands.go` next to `main.go` holds the thin cli command bodies; the root
`*_test.go` files build the binary once and drive it end to end against
`internal/hsehtest`'s fake herdr socket.

## config

`~/.config/herdr/plugins/config/hseh/hseh.toml` (herdr hands popups and
actions this directory as `HERDR_PLUGIN_CONFIG_DIR`). every key is optional:

```toml
popup_width = "85%"           # "N%" of the terminal, or a cell count like 160
popup_height = "80%"
preview_poll_ms = 500         # live preview refresh while a pane is selected
wide_preview_min_columns = 100 # narrower popups stack the preview under the list
trace_file = "/tmp/hseh.trace" # see measuring speed
```

`popup_width`/`popup_height` apply to `hseh launch` (the `plugin_action`
keybinding). the `[[panes]]` sizes in `herdr-plugin.toml` are herdr's fallback
when something else opens the pane without a size. a bad value is reported in
the popup footer and that dimension falls back to its default.

colors follow the `[theme]` in herdr's own `config.toml`, and the sidebar rows
and status indicator style follow its `[ui]` section, so those need no hseh
config.

## spaces rows and preview

each spaces row is one line: agent status, a source badge (`herdr` for a live
workspace, `template` for an unopened definition), the name, git branch and
status, and the absolute path. the path is the git checkout root when the
active pane is inside a repository, otherwise the pane's directory; templates
show their `working_dir`. the path column disappears when the list is narrower
than 45 cells, and a row that still does not fit is cut, not wrapped.

the preview lists that directory with
`eza --icons=always --color=never --group-directories-first -a -F` when `eza` is
on `PATH` (optional; install it for icons), otherwise a plain builtin listing.
color comes from herdr tokens, not eza; `-F` marks directories with a trailing
`/` so the picker can paint them. the listing is read once per selection and
never polls. agents keep their live pane preview.

## measuring speed

for a local `./hseh`, set `HSEH_TRACE` to a file path and hseh appends one line
per timed event (socket calls, git status, preview reads, update/view
durations, and first-time startup milestones). unset, the hooks cost nothing:

```sh
export HSEH_TRACE=/tmp/hseh.trace
```

herdr-spawned popup and action processes do not inherit `HSEH_TRACE`. put the
path in `hseh.toml` instead (`trace.Enabled` reads `config.Load().TraceFile`):

```toml
# ~/.config/herdr/plugins/config/hseh/hseh.toml
trace_file = "/tmp/hseh.trace"
```

useful summaries:

```sh
grep milestone /tmp/hseh.trace              # time to list, time to preview
grep socket.call /tmp/hseh.trace | sort -t= -k2 -n | tail   # slowest herdr round trips
grep git.status /tmp/hseh.trace | sort -t= -k2 -n | tail    # slowest repos
```

`process.start` carries `since_exec_ms` (time spent before `main`, which is
bubble tea's package init querying the terminal background color; a terminal
that never answers stalls there for 5s) and `unix_ms`, so the `launch` and
`popup` processes of one keypress can be lined up.

### the 100ms herdr cliff

herdr's api server reads a request byte by byte in non-blocking mode and, when
the first read finds nothing, sleeps `CONNECTION_POLL_INTERVAL` (100ms) before
looking again (`src/api/server.rs`, `read_request_line`). whether a call takes
0.5ms or 100ms is a race between the client's write and the server's first
read, and processes herdr itself spawns almost always lose it. hseh copes two
ways in `internal/herdr/socket.go`:

- the request is written before anything else touches the connection
  (the continuity witness is read while the reply is in flight).
- the calls a person is waiting on are hedged: the first `session.snapshot`,
  a selection-change `pane.read` (including the very first preview after the
  snapshot lands), and `launch`'s `plugin.pane.open`. if no reply
  lands within 2ms the same request goes out on a second connection and the
  first reply wins. `plugin.pane.open` is safe to duplicate because herdr refuses
  a second popup with `ui_busy`, which `launch` treats as success. background
  polls are not hedged (a late refresh is invisible and the duplicate would only
  cost herdr work), and mutating calls (`workspace.focus`, `agent.focus`, ...)
  are never resent.

with both, keypress to list measures about 12 to 18ms instead of about 210ms.

benchmarks for the cpu side (filtering, layout, view, key handling):

```sh
go test -run xxx -bench . -benchmem
```
