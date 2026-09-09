# hseh

i miss [sesh](https://github.com/joshmedeski/sesh) so i made a shittier one for herdr.

standalone linux herdr space/agent picker. fuzzy search, live previews, git branch/status, reusable spaces. only spaces and agents.

needs go 1.27.1, herdr >=0.9.0, nerd fonts >=3.5 for claude/openai icons.

```sh
git clone https://github.com/regutierrez/hseh.git
cd hseh
go build -o hseh .
herdr plugin link "$PWD"
```

then `herdr plugin action invoke hseh.spaces`

tab changes view. enter switches. esc cancels.
