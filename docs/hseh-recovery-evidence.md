# hseh restart recovery evidence (checkpoint 13)

Binary sha256: `7db552b08a8308ec5a2b966145eb7defecae3f3d695cd63cd9817b6443cf84be`
Recovery driver sha256: `aa8dd52df957fa19730859a57306d9379f2267aa2df4bf8b63a482a80ba60196`
Mouse driver sha256: `95bcf3f9d99e1dc607a8d122f3ed52c17e3f2ab7d8164772b80a3d705f7077ee`

```
go test -overlay /tmp/hseh-oracle-9lBPVV/overlay13.json -run ^TestOracleRecoverRejectsEmptyWorkspaceWithCreate$ -count=1 ./...
go test -count=1 ./... && go vet ./...
go build -o hseh .
python3 docs/evidence/v10/run_restart_recovery.py
python3 docs/evidence/v9/run_mouse_width.py
python3 docs/evidence/v7/run_cli_plugin_parity.py
python3 docs/evidence/v8/run_done_seen.py --nav-only --out docs/evidence/v10/nav-only-smoke
```

Empty `--workspace=` / `--workspace ""` with `--create` is an error. Overlay13 PASS.

Open and recover share `createReusableSpace` (preflight, create, persist, layout).

## Isolated cold restart (`docs/evidence/v10/`)

`herdr api snapshot` succeeded as `session_snapshot` with 2 workspaces. Owned server stop waited until the socket and process were gone. Marker baseline after restart: mark1=1 mark2=1.

| Step | Result |
|---|---|
| `hseh open def-iso-1` | refused recovery commands; mark1 stayed 1 |
| `hseh recover def-iso-1 --workspace w1` | `recovered reconnect def-iso-1 w1`; mark1 stayed 1 |
| associations after reconnect | current `def-iso-1`→w1; unresolved sibling `def-iso-2`→w2 |
| `hseh open def-iso-2` | still refused |
| `hseh recover def-iso-2 --create` | `recovered create def-iso-2 w3`; mark2 1→2 |
| repeat `--create` | `recovered focus def-iso-2 w3`; mark2 stayed 2 |
| associations after create | current def-iso-1 w1 and def-iso-2 w3; unresolved empty |

Direct popup 99 hid preview ticks; 100 showed `BETA_TICK`. Hosted unselected-beta click kept `w1` until Enter. v7 parity PASS. Nav-only PASS.
