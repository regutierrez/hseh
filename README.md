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
