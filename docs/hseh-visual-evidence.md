# Selected blocks, view tabs, and live preview — visual acceptance

Binary SHA256: `bba7f19cf6782a2c52a4ee932ebae58e27dbd2878fee7e7d4f908c949f74125d`
Driver SHA256: `4cb7849d4f3769a5a6b7a1e6f983d8e16bb16640017f214c1adf4ffaf39324b1`

## Changes

- Gray full-width selected item blocks replace the pointer. Selected text uses a uniform light foreground so token styles cannot erase the background. Unselected token styles remain intact.
- Spaces / Agents / All are always shown as tabs. The active tab has a blue background. Search/status occupies a separate row; mouse hit testing uses the same header offset.
- Live terminal previews crop horizontally rather than reflow terminal grids. They show the last available source rows, preserve SGR across cropped rows, and reset styles at column boundaries. Definition/error previews still wrap from the top.

## Verification run

```
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
go build -o /tmp/hseh-visual-build-jzmhaJ/hseh .
python3 docs/evidence/v11/run_visuals.py /tmp/hseh-visual-build-jzmhaJ docs/evidence/v11/results
```

All passed. The staging directory also contains the plugin manifest. The driver links that staging directory only in an isolated HOME/XDG/Herdr session. It checks the user registry hash before and after. No model calls.

The real hosted terminal test checks selected gray background cells, active tab background cells as Tab cycles, query input, last-line preview updates from a wider/taller source terminal, mouse selection without focus, Escape without focus change, and Enter focusing the selected workspace.

`docs/evidence/v11/results/` holds complete terminal streams and reconstructed text frames. `spaces.png` is a rendering of captured terminal cells/backgrounds for visual inspection, not a human screenshot. Foreground colors in that diagnostic image are simplified; SGR preservation is checked separately in Go tests. The grid fixture and style assertions use ASCII; Unicode cell widths are covered by Go tests.

Earlier visual drivers/captures that assert a `>` pointer or one-row header describe earlier builds. Use the v11 driver for this UI.
