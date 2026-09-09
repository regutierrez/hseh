# hseh width+mouse evidence (checkpoint 12)

Binary sha256: `4ef64a030b1e42765cb9298ea56d16c2f12d376bb6e3de5a60af5f7deb1ab2a6`
Driver sha256: `7782c6f143e84fd9888d5a9423d72bdda4ad51042cbf017ecfd88c418b1f40a6`

```
go test -count=1 ./... && go vet ./...
go build -o hseh .
python3 docs/evidence/v9/run_mouse_width.py
python3 docs/evidence/v7/run_cli_plugin_parity.py
python3 docs/evidence/v8/run_done_seen.py --nav-only --out docs/evidence/v9/nav-only-smoke
```

Default `wide_preview_min_columns` is 100 (popup content width). Unit tests: 99 hides preview/no read; 100 shows/read. Hosted mouse used isolated `wide_preview_min_columns = 40` so the 85% popup (~77 cols) can show preview.

## Hosted mouse (`docs/evidence/v9/`)

| Check | Result |
|---|---|
| Click alpha (was not selected) | `> · · alpha`; preview `ALPHA_MARK`; API focus still `w1` |
| Escape | focus still `w1` |
| Reopen click beta + Enter | `> beta`; focused `w2` |

Nav-only smoke PASS. Reusable-space parity PASS (`opened focus def-iso-1 w2`, marker once).
