# hseh picker evidence (checkpoint 8)

Binary sha256: `4c07f0c1647acd1dcfce0feb720d174b544555a7f3a695558f6d0f0428ba7609`
Driver sha256: `00b282d579685679fe7bee3fec2a574917ae3758c602c19133ac3d7fce22afcd`

```
go test ./... && go vet ./...
go build -o hseh .
python3 docs/evidence/v5/run_integrated.py
```

Isolated test config: `wide_preview_min_columns = 40` (not a product default).

## No-model-call coverage

| Check | Result |
|---|---|
| CRLF preview | overlay6 PASS |
| Hosted `>` beta + ticks | dumps 0–3; frame-dump-3 `5dec6ede…` |
| Narrow: no ticks, selection stays | asserted in driver; narrow-dump `67a29e4a…` |
| Resume wide: ticks + selection | asserted; resume-wide `f5f98389…` |
| Escape: w1 still focused | after-escape |
| **Enter exact beta w2** | after-enter `93fb2242…` `w2` focused |
| Query keep across views / clear | `TestQueryRetainedAcrossViewsAndClearedOnRebuild` |
| Preview does not mutate MRU | `TestPreviewDoesNotChangePreselectHistory` |
| Disappeared target not auto-selected | `TestSelectionDisappearsWhenTargetGone` |
| Offscreen multiline selected row | `TestPreviousAgentOffscreenStaysVisible` |
| pane.read count wide 1 / narrow 0 extra / resume 2 | `TestNarrowStopsPreviewReadsAndWideResumes` |
| Verified move after snapshot prune keeps MRU | `TestVerifiedMoveAfterSnapshotKeepsPublicMRU` |
| Dead pane not offered | `TestPreselectSkipsAbsentHistoryTarget` |
| Idle Pi | `run_idle_pi.py` (idle only) |
| `done`→seen | **blocked** pending user permission for one Pi turn |

User `plugins.json` hash unchanged.

## Overlay4

Private `len(history.Agents)==0` is not required. Public tests replace it.
