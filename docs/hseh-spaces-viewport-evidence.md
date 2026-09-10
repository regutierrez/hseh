# Sesh-style Spaces viewport (issue #3)

Staging build: `a732cc934e32f1cf4e72de1a49ec80f128f9f7cc24afa479934a03984e8aec9c` (`/tmp/hseh-v15-stage/hseh`, the installed plugin was not touched).

- Spaces rows are one line: status slot, source badge (`󰳆 herdr` for live workspaces, `󰆏 template` for unopened definitions), name, git branch/status, absolute path. Definition descriptions left the row and stay in search text. Unresolved definitions carry a `(recovery needed)` tag; the exact `hseh recover` commands moved to the preview header and to a new `recovery` field in `hseh list --json`, which also gained `source` and `path`.
- A live space's path is its git checkout root when the active pane is inside a repository (`gitinfo` now runs `git rev-parse --show-toplevel` first), otherwise the pane directory. Templates show `working_dir` and get the same git pass.
- The Spaces preview lists the selected directory once per selection through `internal/dirlist`: `eza --icons=always --color=always --group-directories-first -a <dir>` (one entry per line, no long format) with a 2s process-group timeout when `eza` is on `PATH`, otherwise a builtin `os.ReadDir` listing. It never polls and never calls Herdr. Agents keep the live pane preview.
- The path column hides when the list content is narrower than 45 cells (`pathColumnMinWidth`, hardcoded). Spaces rows and listing rows truncate instead of wrapping, so a row is always one line.

## Verification run

```
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go build -o /tmp/hseh-v15-stage/hseh . && cp herdr-plugin.toml /tmp/hseh-v15-stage/
python3 docs/evidence/v15/run_spaces_viewport.py /tmp/hseh-v15-stage docs/evidence/v15/results
python3 docs/evidence/v11/run_visuals.py /tmp/hseh-v15-stage docs/evidence/v15/v11-regression
```

All passed. Both drivers use an isolated HOME/XDG/Herdr session and check the user plugin registry hash before and after. No model calls.

## New unit coverage

`internal/dirlist`: builtin fallback ordering (directories first, hidden files shown), missing directory, empty directory copy, output cap, fixed eza arguments, eza stderr surfaced, timeout returns promptly. `internal/picker/viewport_test.go`: one-line herdr and template rows, checkout-root path, badge alignment across sources, path column hiding and narrow highlighting, JSON `source`/`path`/`recovery`, directory read once per selection with recovery header, no poll tick after a listing, isolated listing errors with Enter still available, truncating listing clip, long rows cut to one line, git for template directories. Tests that exercised pane previews on spaces rows now use agent rows; `TestFirstPreviewReadIsHedged` moved to the Agents view because spaces no longer read panes.

## Hosted terminal run (`docs/evidence/v15/results/`)

`run_spaces_viewport.py` creates a git repository with a subdirectory, a template directory, and a recovery directory. It opens template `rec` through the staged CLI, cold-restarts the isolated server so that association becomes unresolved, then attaches a 200×40 client. 200 columns matter: Herdr keeps a 26-column sidebar, the popup takes 85% of the rest, and the list gets half of that, which comfortably clears the 45-cell path threshold; 90 columns is used for the narrow case.

| Capture | Checked |
|---|---|
| `list.json` | `alpha` is `source: herdr` with `path` = the repository root, not the pane's subdirectory; `gamma` is `source: template`; `rec` carries both `recovery` commands; every item has exactly one row |
| `spaces-wide` | active tab; three `herdr` and two `template` badges; alpha's row shows the checkout root and `main`; the `rec` template row shows `(recovery needed)` with its path; the description string is not rendered |
| `template-preview` | query `gamma` + Up selects the template (rail + bold); the preview lists `GAMMA_MARKER.txt`; focus unchanged |
| `recovery-preview` | query `recovery` selects the unresolved template; the preview shows `hseh recover def-rec --create` above the listing of `REC_MARKER.txt` |
| `space-preview` | query `alpha` lists the repository root (`README_MARKER.md`, `sub`), not pane output; focus unchanged |
| `spaces-narrow` | resized to 90 columns: rows keep names, the path column is gone, an over-long row is cut with `…` on one line, and the 25-cell preview still shows `README_MARKER.md` |
| `spaces-wide-again` | resized back to 200: the path column returns; focus unchanged |
| `before-enter`, `enter` | Enter on the selected `gamma` template creates one workspace labelled `gamma` and focuses it |

`spaces-wide.png` is a rendering of captured terminal cells, not a human screenshot.

## v11 driver update (`docs/evidence/v15/v11-regression/`)

`run_visuals.py` asserted that the spaces preview showed the beta pane's live grid and that it polled. Both now hold the opposite: the preview lists beta's directory (`grid.py` present) and `BETA_LAST_` never appears, including after a further 1.2s. Two checks in that driver were already stale on `main` before this change and were updated so it runs at all: the selected row is marked by the accent rail plus bold since the rail UI replaced gray blocks, and the active tab background arrives as truecolour (`48;2;137;180;250`) or xterm 74 depending on the terminal profile. `docs/evidence/v12/run_details.py` still asserts the gray block and was left untouched.

## Known limits

- With the default 50/50 split the path column needs a list of at least 45 cells, so a popup narrower than about 94 columns hides it. Dragging the divider or a wider terminal restores it.
- Without `-l` the listing shows names only; permissions, sizes and dates are not available in the preview.
- The first git pass runs when the snapshot lands; if template definitions load after it, their branch appears on the next 3s git tick unless the catalog load triggers a pass first (it does when no pass is running).
