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

tab changes view. enter switches. esc cancels.

## measuring speed

set `HSEH_TRACE` to a file path and hseh appends one line per timed event
(socket calls, git status, preview reads, update/view durations, and first-time
startup milestones). unset, the hooks cost nothing. put it in your shell profile
or herdr's env so popups inherit it:

```sh
export HSEH_TRACE=/tmp/hseh.trace
```

useful summaries:

```sh
grep milestone /tmp/hseh.trace              # time to list, time to preview
grep socket.call /tmp/hseh.trace | sort -t= -k2 -n | tail   # slowest herdr round trips
grep git.status /tmp/hseh.trace | sort -t= -k2 -n | tail    # slowest repos
```

`process.start` carries `since_exec_ms`: time spent before `main`, which is
bubble tea's package init querying the terminal background color. a terminal
that never answers stalls there for 5s.

herdr answers within a millisecond only when the request arrives immediately
after connect, so the socket client writes the request before reading the
continuity witness. do not put work between dial and write.

benchmarks for the cpu side (filtering, layout, view, key handling):

```sh
go test -run xxx -bench . -benchmem
```
