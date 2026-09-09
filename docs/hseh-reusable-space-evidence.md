# hseh reusable-space evidence (checkpoint 11)

Binary sha256: `d6e12fe540e264de16135dc6658136289444f748e8e8578d21b62e62c93b8381`
Driver sha256: `1dd842ef51e257c12fd5b7b33a6272f79ea42099c04bc888f2b16d1b5e2254a8`

```
go test -count=1 ./... && go vet ./...
go test -overlay /tmp/hseh-oracle-9lBPVV/overlay9.json -run ^TestOracle -count=1 ./...
go build -o hseh .
python3 docs/evidence/v7/run_cli_plugin_parity.py
```

Overlay9 PASS. Isolated HOME/XDG. User plugins.json `3947d1ed…` unchanged. No `HERDR_PLUGIN_*` on CLI. Test-only `wide_preview_min_columns = 40`.

## Isolated Herdr

| Check | Result |
|---|---|
| Preview column (v5 CellScreen) | `tab root` in popup-preview.txt; raw+dump saved; marker file absent before Enter |
| Popup Enter | focused `w2` (`iso-one`); marker once |
| Tabs/panes | `root`,`web`; cwd proj, proj/web, proj/web/app |
| CLI open (socket/session/XDG only) | after-cli-focus focused ids `['w2']` |
| Plugin reopen query/select `iso-one` | after-plugin-reopen focused ids `['w2']`; one iso-one workspace; marker once |

`TestOpenReusableSpaceReportsSubmittedCountWhenLaterCommandFails` is a controlled API test, not compiled CLI.

Restart association still stops. `done`/seen, width/mouse, macOS runtime still pending.
