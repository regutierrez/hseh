# hseh — Space and Live Agent Switcher

Status: Agreed specification synthesized from the design interview. Product decisions and testing seams are confirmed; technical unknowns below remain verification gates.

## Problem Statement

Switching between Herdr spaces and active coding agents requires too much navigation. Short, single-line picker entries do not show enough of the status and labels already visible in Herdr's sidebar. Returning to the previous space or agent should be predictable, regardless of whether the last switch happened through a picker, a shortcut, or the sidebar.

Frequently used projects also require repeated workspace setup. The user wants fixed-directory space definitions that recreate tabs, panes, and startup commands when needed, but simply focus a suitable existing workspace when it is already open.

The user does not want to adopt an unreviewed community plugin or maintain a full tmux-oriented sesh fork. The implementation should be small, Herdr-native, reviewable, and independent of personal dotfiles.

## Solution

Build hseh as a standalone Herdr plugin with one searchable popup for open spaces, open agents, and fixed-directory reusable space definitions.

The popup has Spaces, Agents, and All views. Spaces use most-recently-used (MRU) ordering. Agents use Herdr's attention priority. Each view preselects its previous target. Entries reproduce Herdr sidebar information as flat multiline rows, not as an expandable tab/pane tree.

Moving selection updates a read-only preview. Enter focuses a live target or opens a reusable space. Browsing must not change Herdr focus, mark agents seen, or pollute MRU history.

Reusable spaces use hand-written TOML definitions based on reviewed Herdr Plus code. Opening a definition focuses its associated live space, adopts an eligible unassociated space, or creates its configured layout. Startup commands run only during creation.

The initial audience is the user's workflow, with publishable source and personal settings kept outside the implementation. The intended platforms are Linux and macOS; support is claimed only after testing.

## User Stories

1. As a Herdr user, I want one popup for spaces and live agents, so that I can navigate without changing tools.
2. As a Herdr user, I want the default view to show spaces, so that common workspace switching is immediate.
3. As a Herdr user, I want dedicated Agents and All views, so that I can choose the scope of my search.
4. As a Herdr user, I want separate launch actions for each view, so that I can bind shortcuts to my workflow.
5. As a Herdr user, I want only agents currently open in the current Herdr session, so that selection always targets live work.
6. As a Herdr user, I want multiline entries using Herdr's labels and status, so that I can identify work without opening it.
7. As a Herdr user, I want missing metadata omitted rather than invented, so that the picker stays truthful.
8. As a Herdr user, I want flat results, so that I do not have to navigate a nested layout tree.
9. As a Herdr user, I want spaces ordered by recent use, so that familiar work stays easy to reach.
10. As a Herdr user, I want agents ordered by attention priority, so that blocked and newly completed work remains visible.
11. As a Herdr user, I want the previous space preselected, so that opening the popup and pressing Enter takes me back.
12. As a Herdr user, I want the previous agent preselected even below higher-priority entries, so that returning to it is predictable.
13. As a Herdr user, I want MRU to include sidebar and shortcut navigation, so that history reflects how I actually work.
14. As a Herdr user, I want preview browsing excluded from MRU, so that browsing does not overwrite meaningful history.
15. As a Herdr user, I want history scoped to the current Herdr session, so that unrelated sessions do not affect selection.
16. As a Herdr user, I want valid history retained across restarts, so that returning to work remains convenient.
17. As a Herdr user, I want closed or replaced targets removed from history, so that Enter cannot select unrelated work.
18. As a Herdr user, I want fuzzy search across displayed information and definition paths, so that I can find work using what I remember.
19. As a Herdr user, I want the query preserved when switching views, so that I do not need to type it again.
20. As a Herdr user, I want each popup launch to start with an empty query, so that old filters do not silently hide work.
21. As a Herdr user, I want selection tied to target identity during refresh, so that status updates cannot redirect my action.
22. As a Herdr user, I want a disappeared selection cleared, so that another row cannot unexpectedly receive Enter.
23. As a Herdr user, I want a preview of the selected live target, so that I can inspect it without changing focus or marking it seen.
24. As a Herdr user, I want live previews to update, so that I can see current terminal output.
25. As a Herdr user, I want definition previews to show setup without executing it, so that browsing has no startup side effects.
26. As a Herdr user, I want preview failures isolated from navigation, so that I can still focus a valid target.
27. As a Herdr user, I want narrow popups to show results only, so that the list remains readable without a squeezed preview.
28. As a Herdr user, I want Escape to dismiss the popup without navigation, so that cancelling is safe.
29. As a Herdr user, I want reusable fixed-directory spaces, so that common project setups are repeatable.
30. As a Herdr user, I want to author tabs, split panes, directories, and commands in TOML, so that setup remains explicit and portable.
31. As a Herdr user, I want stable definition IDs generated automatically, so that I do not need to manage identifiers manually.
32. As a Herdr user, I want file and display-name changes to preserve identity, so that renaming does not create duplicate spaces.
33. As a Herdr user, I want different definitions for the same directory to remain distinct, so that development and operations setups can coexist.
34. As a Herdr user, I want an already-associated space focused rather than recreated, so that reconnecting does not rerun commands.
35. As a Herdr user, I want suitable existing unassociated spaces reused automatically, so that hseh avoids unnecessary duplicates.
36. As a Herdr user, I want ambiguous directory layouts left unassociated, so that an unrelated workspace is not silently claimed.
37. As a Herdr user, I want concurrent opens to create at most one associated space, so that simultaneous actions cannot duplicate setup.
38. As a Herdr user, I want changed definitions applied only on new creation, so that live agents and commands are not disturbed.
39. As a Herdr user, I want invalid definitions reported without blocking other targets, so that one config mistake does not disable navigation.
40. As a Herdr user, I want directory errors caught before creation, so that avoidable mistakes do not leave partial workspaces.
41. As a Herdr user, I want partial creations retained with a clear failed step, so that failure recovery does not kill useful processes.
42. As a Herdr user, I want startup commands submitted only once per creation attempt, so that automatic retries cannot duplicate processes.
43. As a script author, I want open-by-definition and JSON listing commands, so that automation uses the same behavior as the popup.
44. As a Herdr user, I want the existing MRU quick-switch action preserved, so that adopting hseh does not break muscle memory.
45. As a maintainer, I want copied code pinned, attributed, and reviewed, so that its origin and local changes are clear.
46. As a maintainer, I want source builds without silent binary-download fallbacks, so that installation runs the code I reviewed.
47. As a maintainer, I want real popup acceptance tests before reusable-space work, so that the core interaction is proven early.

## Implementation Decisions

### Ownership and implementation base

- Use a standalone Go application with Bubble Tea, packaged as a first-class Herdr plugin.
- Treat sesh as a behavioral reference, not the application to fork.
- Copy only needed Herdr Plus definition parsing, validation, layout construction, and relevant tests after focused review.
- Pin the initial Herdr Plus source to revision `f38df3570bea8f7ca71dc1ba11bce3b123d14402`. Retain its MIT copyright and license notice and record copied-source provenance and local changes.
- Do not inherit the upstream installer, Quick Actions, or worktree automation.
- Logical responsibilities are picker presentation, live Herdr integration, focus history, reusable-space loading/resolution, and creation. Avoid separate resolution implementations for CLI and TUI callers.
- Herdr remains the owner of terminal processes, agent detection, status, focus, and existing title metadata. hseh must not mutate Herdr's shared agent-view configuration to sort its own results.

### Scope and presentation

- Operate inside the current Herdr session only. Do not aggregate other local or remote servers.
- Offer Spaces, Agents, and All views, with Spaces as the default.
- Spaces contains live workspaces and unopened reusable definitions. Suppress a separate definition entry when its associated space is already open.
- Agents contains only open agents. All contains live spaces, then agents, then unopened definitions, without an expandable hierarchy.
- Offer separate plugin launch actions for each view and a quick-switch action. Personal keybindings are selected during installation rather than installed implicitly.
- Use a centered popup with configurable size, initially 85 percent width and 80 percent height.
- Follow Herdr's configured sidebar rows, including applicable per-agent overrides, labels, status, metadata, and token styling where supported. Wrap content to available width rather than truncating it to a single line.
- Omit missing values and empty rows. Keep sufficient target identity to distinguish entries. Do not generate descriptions or infer status from terminal output.
- Arrow keys move selection. Tab cycles views in Spaces, Agents, All order; Shift+Tab reverses. Typing searches. Enter accepts. Escape closes without changing focus.
- Merely moving the mouse pointer does not select a row. A left mouse press on a result row selects that row and updates the preview. Enter performs the switch. Clicks on the header, preview column, separators, padding, or outside the list do nothing. Mouse release, drag, and right-click do not accept.

### Ordering, search, selection, and refresh

- Order live spaces by MRU. Put unopened definitions after live spaces, sorted by name. Use Herdr workspace order when history cannot distinguish spaces.
- Order agents by Herdr attention priority: blocked, done/unseen, working, idle/seen, unknown. Use its state-change sequence as the priority tie-breaker, subject to version-specific verification.
- Agent priority and previous-target selection are separate: scroll to and select the previous agent even when it is below higher-priority rows.
- Spaces preselects the previous space. All preselects the previous agent when launched from an agent pane, otherwise the previous workspace.
- If the previous target is unavailable, select the first non-current result. If only the current target exists, select it. Empty results have no Enter action.
- Fuzzy-match displayed text, including status and location, plus definition descriptions and paths. Search runs locally without model calls or terminal-output reads for ranking.
- While searching, match quality determines ordering; normal view ordering breaks ties. Clearing the query restores normal ordering using current data.
- Keep the query when switching views, but reset it on each popup launch.
- Refresh membership and status without changing the relative order of existing rows merely because focus or status changes. Recompute ranking when the query changes or the popup reopens.
- Selection follows stable target identity, never row index or rendered text. If the selected target disappears, clear selection and require a fresh choice.
- Keep the popup's launch context stable if navigation occurs outside hseh while it is open. External navigation still updates MRU. Cancelling must not restore an obsolete launch focus.

### Read-only previews

- Show the preselected target's preview immediately on opening.
- An agent preview shows that agent pane's visible terminal output.
- A Spaces preview lists the selected row's directory, read once per selection: `eza --icons=always --color=always --group-directories-first -a` when `eza` is installed, otherwise a builtin listing. Arguments are fixed, not user-configured. A live space lists its Git checkout root when the active pane is inside a repository, otherwise the active pane's directory; a definition lists its `working_dir`. Live pane output and definition layout text are not shown in Spaces.
- Spaces rows are one line: status slot, a source badge (`herdr` for live workspaces, `template` for unopened definitions), name, Git branch/status, and the absolute path. The path column hides when the list is narrower than 45 cells; long rows are truncated, never wrapped. Definition descriptions remain searchable but are not displayed.
- Show previews beside results when the popup content is at least 100 columns wide (`wide_preview_min_columns`, default 100). Narrow popups always hide the preview; there is no results/preview toggle. Stop preview reads when resized to narrow and resume the selected preview when resized back to wide, preserving selection and Herdr focus.
- Refresh only the selected visible preview. Use read-only Herdr APIs; reads must not focus targets or mark agents seen.
- Preserve terminal styling safely; terminal output is display data, not instructions to execute.
- A failed preview shows an error and clears stale output. It does not prevent navigation to a target that remains valid.
- Browsing has no navigation or MRU side effects. Enter performs the real focus operation, whose normal Herdr seen-state effects are expected.

### MRU and target identity

- Incorporate the existing workspace-MRU plugin's behavior and replace its deployment only after hseh passes acceptance testing.
- Preserve its quick-switch behavior: first invocation goes to the previous workspace; continued invocations within its existing 500-millisecond cycle traverse workspace order. This is distinct from popup list ordering.
- Maintain one owned history system with separate space and agent histories, scoped to the current Herdr session.
- Record actual focus changes from Herdr navigation, not only hseh actions. Exclude the hseh popup itself.
- Persist history across hseh invocations and Herdr restarts where identity can be validated. Drop closed targets and never equate reused handles with old targets without evidence.
- Track live agent identity, including its session reference where available. A replacement agent in the same pane must not inherit the previous occupant's history. Handle moved panes without confusing their old and new handles.
- Persist association and history state outside user configuration. Herdr metadata may expose associations, but its durability must not be assumed.

### Reusable definitions and configuration

- Store settings and TOML space definitions in Herdr's managed plugin config directory. Keep runtime state separate. Personal configuration may later be managed through chezmoi.
- Support fixed directories only. Do not implement the Herdr Plus directory-prompt sentinel.
- Reuse the relevant Herdr Plus definition fields for names, descriptions, directories, ordered tabs, split panes, labels, ratios, and startup commands. Retain its four-pane-per-tab limit for v1.
- Retain applicable upstream directory inheritance and relative-path behavior only after review and tests establish the exact contract. Require a configured fixed project directory rather than silently turning an omitted directory into a generic template.
- Automatically add a missing stable ID to its definition file, preserving comments and formatting. Never replace an existing ID. Read-only files missing an ID and duplicate IDs are configuration errors, not reasons to generate temporary identities.
- Author definitions directly in TOML. Do not build forms, a configuration editor, or live-layout capture.
- Surface malformed and invalid definitions visibly by file while keeping live targets and other valid definitions usable.
- Definition changes do not reconcile or modify existing workspaces. They apply on the next creation.

### Create-or-focus and automatic association

- Logical identity is definition ID plus resolved directory. Display names and definition filenames are not identity.
- Different definitions can own different workspaces at the same directory.
- First focus an exact live association. Otherwise consider eligible unassociated workspaces. Never claim a workspace associated with another definition.
- For unassociated workspaces, use an available Git checkout path as the directory signal. Otherwise require every pane to report the same directory as the definition. Mixed or missing directory information means no automatic match. Compare resolved paths, not labels.
- When several eligible workspaces match, adopt the most recently focused; use Herdr workspace order when history cannot distinguish them.
- Adoption records the association and focuses the workspace without running commands or changing its layout. Do not prompt for adoption in v1.
- If no eligible workspace exists, validate the definition and all required directories before creating its layout.
- Serialize the create-or-focus operation within the current Herdr session and recheck live associations inside the lock to prevent duplicate concurrent creation.
- Run commands only as part of new creation. Use ordinary configured shell commands rather than a separate agent-launch configuration system. Do not add permission-bypass flags.
- Creation succeeds when layout operations and startup-command submission succeed. Do not wait for application health or agent readiness as an additional product-level acceptance condition.
- If creation fails after mutations start, retain the partial workspace and report the failed step. Do not automatically delete it or retry commands. Reopening focuses it; rebuilding requires the user to close it through Herdr first.
- After a Herdr server restart, current associations become unresolved identities. They are not live ownership and are not focus targets. `hseh open` and picker Enter block only that identity and print the exact recover commands. Unrelated definitions still open normally.
- Recover with `hseh recover <definition-id> --workspace <live-workspace-id>` or `hseh recover <definition-id> --create`, exactly one choice. Reconnect records the chosen live workspace and focuses it with no layout or startup commands. Mixed directories are allowed by explicit choice. Never steal a workspace that currently belongs to another definition.
- Explicit create is only for an unresolved identity. If a current exact association already exists, recover only focuses. Repeat recover after partial create must not replay commands. Recovering one identity must leave sibling unresolved records in place. A later restart moves remaining current records into unresolved without dropping those siblings.
- Popup and `hseh list --json` keep unresolved definitions visible, say recovery is needed, and show those exact command forms: the row carries a "recovery needed" tag, the preview and the JSON `recovery` field carry the commands. No extra popup mode.

### CLI and installation

- Provide `hseh open <definition-id>`, `hseh recover <definition-id> --workspace <live-workspace-id>` or `--create`, and `hseh list --json` alongside the popup. Open uses ordinary create-or-focus and cannot bypass unresolved provenance.
- Keep CLI operations scoped to the current Herdr session. Treat JSON output as a machine-readable public interface; finalize its schema before implementation tests depend on it.
- Build reviewed source at a pinned revision. Do not silently download an unpinned latest binary when a build fails or a toolchain is absent.
- Defer prebuilt release distribution until it can be tested. Do not create or publish the remote repository as part of writing this specification.

## Testing Decisions

### Confirmed acceptance seams

- Primary acceptance seam: invoke the built hseh application through its CLI and Herdr plugin popup in an isolated, named Herdr test session. Observe caller-visible output, focus, status, resulting workspace topology, submitted commands, and persisted behavior across invocations.
- Use a terminal driver and rendered popup inspection for keyboard interaction, multiline layout, resizing, preselection, and previews. Text snapshots alone do not prove a usable visual layout.
- Use a controlled Herdr API peer only for failures and races that are difficult or unsafe to force in a live server. Exercise the same application boundary rather than mocking each internal function.
- Good tests assert observable behavior, not private helper calls, internal struct layout, or incidental implementation order. Share fixtures and application entry points instead of building separate CLI and TUI test harnesses for identical business logic.

### Coverage

- Picker presentation and navigation: views, query retention/reset, fuzzy ranking, multiline wrapping, configured tokens, missing metadata, results-only narrow mode, and immediate preview of the preselected target in wide mode. Resizing hides/resumes preview reads without changing selection or Herdr focus.
- Focus safety: browsing and cancellation leave focus and seen state unchanged; Enter focuses the exact intended target; external navigation while open is not undone on cancellation.
- Ordering and history: space MRU, agent priority, previous-target preselection, fallback selection, history from external navigation, no popup history pollution, and preserved quick-switch cycling.
- Dynamic state: refresh without selection drift, disappearance clearing selection, agent replacement, pane moves, and targets closing between selection and acceptance.
- Previews: correct agent pane, selected-only refresh, styling, stale-output clearing, and isolated read failures. Spaces directory listings: eza versus builtin fallback, timeout, single read per selection, missing directories, and path-column hiding.
- Definitions: parsing, per-file error isolation, fixed-directory requirements, directory inheritance, spaces and special characters in paths, four-pane limit, and automatic IDs without formatting loss.
- Resolution: exact association, rename stability, distinct definitions sharing a directory, conservative adoption, multiple matches, and refusal to take another definition's workspace.
- Creation: preflight validation, concurrent duplicate prevention, startup commands submitted once, reconnect/adoption submitting none, and partial failure retaining a usable associated workspace.
- Public CLI: open-by-ID parity with popup behavior, useful failures, valid JSON output, and session scoping.
- Durability: restart handling must not misidentify reused workspace/pane handles or replacement agents. Tests must establish which associations survive and how unverifiable state is rejected.
- Installation: source-build failure does not trigger a binary download; old MRU deployment remains untouched until an explicit tested migration.

### Prior art and staging

- The new repository is empty and has no existing tests, implementation, ADRs, or domain glossary. This document establishes the initial vocabulary and contracts.
- Reuse relevant reviewed Herdr Plus parsing, directory, layout, and picker tests as prior art, adapting behavior where this spec intentionally differs.
- Reuse the local workspace-MRU tests for focus history, cycle timing, removals, and externally initiated focus changes.
- First prove live listing, priority/MRU selection, multiline rendering, and read-only previews in a real popup. Then add reusable-space creation and failure handling.
- Run tests against isolated resources created for the tests. Do not close, rename, or inject commands into the user's existing workspaces or agents.
- Validate Linux and macOS explicitly before claiming support for either. Do not claim runtime or visual verification from source inspection alone.

## Out of Scope

- Saved or closed agent-session discovery, resumption, and archive indexing.
- Cross-server aggregation, remote connection management, or navigation between different Herdr sessions.
- Windows support in v1.
- Zoxide, arbitrary recent-directory discovery, and frecency scoring.
- Directory-prompt templates, template inheritance, layout capture, and configuration editing UI.
- Generated task summaries, model calls for picker content, or status inferred from terminal output.
- Nested tab/pane navigation trees and full workspace-layout previews.
- Quick Actions, worktree creation or automation, tmux compatibility, and a complete sesh port.
- Closing spaces, stopping agents, renaming targets, sending prompts, and other management actions from the picker.
- Automatic reconciliation of changed definitions into running spaces.
- Automatic rollback that kills partial workspaces, automatic command retries, and application health supervision.
- User-configured preview commands. The fixed `eza` listing is the only external preview tool.
- Changes to existing Auto Title ownership or Herdr's shared agent-view configuration.
- Implicit keybinding changes, remote repository creation, publishing, and unpinned binary-download fallbacks.

## Further Notes

### Vocabulary

- **Space:** a live Herdr workspace. A space can contain tabs and panes, but the picker does not present them as a navigation tree.
- **Reusable space definition:** a fixed-directory TOML setup from which hseh can create or identify a space.
- **Agent:** an agent currently detected in a live Herdr pane, not an archived conversation.
- **Association:** the link between a definition/directory identity and a live workspace.
- **MRU:** focus recency, not frequency of use or zoxide frecency.
- **Priority:** Herdr's attention ordering, separate from MRU and previous-target preselection.

### Verified research context

- The inspected installed Herdr version was 0.8.2, protocol 20. Its workspace and agent APIs provide live identities, status, labels, and metadata. Pane reads support visible terminal output without marking targets seen.
- Herdr does not expose a canonical directory on every workspace. Conservative adoption is therefore an explicit product rule rather than an assumption that every workspace has a stable root path.
- Sesh was inspected at revision `835b04c5af383be639b4b0bc2c861f58226d3dcb`. Its live tmux sessions use attach recency; zoxide uses frecency. Previous-target preselection is an intentional hseh feature, not something assumed inherited from sesh's picker.
- Herdr Plus supports directory-prompt definitions, but hseh intentionally excludes them after the user's scope revision.

### Technical verification gates

These are not silently solved requirements. Resolve them before depending on their behavior; bring back any required product compromise rather than weakening the spec without agreement.

1. Establish restart-safe server/session and target identity. Stable socket location alone does not prove a workspace or pane handle still identifies the same target after a restart.
2. Establish durable association storage and reconciliation. The inspected metadata API exposes sequencing and optional expiry but does not establish a restart-persistence guarantee.
3. Confirm access to effective sidebar configuration, defaults, per-agent overrides, and token rendering. Reading live labels alone does not reproduce configured row structure.
4. Confirm live event subscription and active-pane resolution for selected-only preview refresh. Do not invent polling intervals before checking the available event surface.
5. Review copied startup-command submission and partial-failure handling against the installed Herdr API. Upstream creation always creates a new workspace and is not a drop-in create-or-focus implementation.
6. Confirm how to distinguish replacement agents when Herdr omits a persistent agent-session reference.
7. Verify that a popup can remain usable while reading other targets and that plugin-pane creation does not leak into focus history.
8. Finalize the public JSON listing shape, safe terminal-output rendering, and formatting-preserving automatic ID insertion before their corresponding acceptance tests.

The intended remote project name is `regutierrez/hseh`; remote creation and publication are deferred. No Linear issue is required for this specification.
