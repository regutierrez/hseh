# Two views, bottom-up results, and agent focus

Installed Linux build: `75802deaae91a51bad9a833cbc0c6918a440c49f752f5bee30c10454ed3f76f1`.

- Removed All from tabs, CLI views, and plugin actions/panes. Backtick-w now invokes `hseh.spaces`; Tab alternates Spaces and Agents.
- Kept tabs at the top. Search is at the bottom left, with the preview extending beside it. Ranked item blocks grow upward; rows within agent blocks retain their order. Arrow keys move in their visible direction. Mouse coordinates use the same rendered block layout.
- Fixed agent acceptance by following successful `agent.focus` with `tab.focus` using the returned agent's tab ID. No cached or pane-derived tab guess is used. Occupant validation remains unchanged.

## Focus bug evidence

`docs/evidence/v14/run_agent_focus.py` uses an isolated named Herdr server, HOME/XDG paths, registry, and socket. Two shell panes report controlled agent metadata through the official API; no model turns run.

The old build failed in `docs/evidence/v14/baseline/outcomes.json`: the server reported the expected workspace/tab/pane, but keyboard input did not reach that pane. Previous checks of focus IDs alone therefore did not prove visible client focus.

The final build passed across workspaces, across tabs in one workspace, and across split panes in one tab. The test enters a harmless marker without submitting it, reads the exact target's viewport, and clears the input. See `docs/evidence/v14/final-focus/outcomes.json` and the captured frames/probes.

## Validation

Passed Go tests, vet, and race tests. Existing hosted details and visual drivers also passed against the same staging build:

- `docs/evidence/v14/final-focus/`
- `docs/evidence/v14/final-details/`
- `docs/evidence/v14/final-visual/`

PNG files reconstruct captured terminal cells; they are not human screenshots. Existing visual checks were updated for the two-view contract; new tests cover the focus bug, not aesthetic choices. The parent reviewed source changes directly because this directory has no Git repository.

The installed binary matches staging byte-for-byte. `herdr config check` passed, reload returned no diagnostics, and plugin action listing confirmed Agents, Spaces, and the separate previous-space action, with no All action. User panes were not focused or given input during validation or installation.
