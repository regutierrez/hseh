# Git details and agent rows

Verified Linux build: `664e88502b66618d36e1085b6bcb593999292553d633cc5e9828dbd547d50cff`.

## Behavior

- Live spaces use the active pane's foreground directory, falling back to its cwd.
- Repository rows show a Git symbol, bold space name, branch, and gray status counts: `+` staged, `~` modified, `?` untracked, `!` conflicts, `↑` ahead, `↓` behind. Clean repositories show no counts. Linked worktrees show their checked-out branch.
- Git reads run separately from snapshot and preview reads. They do not acquire optional Git locks or invoke fsmonitor. Agent-only views do not start Git reads.
- Agent rows show status/harness/tab, then space/abbreviated directory, then the configured Herdr description rows joined together. Custom tokens and per-agent description overrides remain supported.
- Secondary text stays gray across wrapped and selected rows. Selected rows use a lighter gray for readable contrast.
- Status comes from Herdr snapshots, not a second detection system. Against Herdr 0.9.0's terminal theme, the working yellow, blocked bright red, done cyan, and idle green colors and dot shapes match.

## Checks

Passed:

```sh
go test -count=1 -run TestWrappedDetailsKeepMutedSelectedColor ./...
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
python3 docs/evidence/v12/run_details.py /tmp/hseh-details-build-p2Nr5X docs/evidence/v12/final-results
python3 docs/evidence/v11/run_visuals.py /tmp/hseh-details-build-p2Nr5X docs/evidence/v12/final-visual-regression
```

Re-run on build `976a75d02f168a76df13308cb143764e8b6c18d76b356c076b34c373c3c4bf41` (commit after `c7d3dfe`): `run_details.py` now checks the selected row by the accent rail plus bold name, since the rail UI replaced the gray block, and waits 3.5s for the Git count refresh because the Git poll moved to a 3s tick in `6d2b86e`. Everything else in the driver passed unchanged, including the agent rows and Enter focus, with the spaces preview now a directory listing.

`final-results/status-colors.json` records native Herdr and picker foreground values for all four states. The hosted test also checks Git count refresh, bold/gray styling, the three agent rows, read-only browsing, and Enter focus. The regression driver checks tabs, selected backgrounds, bottom-aligned terminal preview cropping, mouse selection, Escape, and Enter.

All hosted checks use temporary HOME/XDG paths, an isolated plugin registry, a named server, and its exact socket. Only owned resources are removed. Status fixtures use the official report-agent API; no model calls ran. This is renderer verification, not a new real-model lifecycle test. PNG files reconstruct captured terminal cells using local fonts; they are not human screenshots. Custom Herdr theme colors and macOS were not verified.

The directory has no Git repository, so a Git diff was unavailable. Changed source was reviewed directly. The final wrapped-text regression initially had an invalid test model field; it was corrected to use the existing visible-items seam before the checks above passed.
