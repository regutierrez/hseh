package picker

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/dirlist"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
	"github.com/regutierrez/hseh/internal/termtext"
	"github.com/regutierrez/hseh/internal/trace"
)

// snapshotPollInterval is the live membership/status refresh interval.
// It is slower than the preview clock: membership changes rarely, terminal output often.
const snapshotPollInterval = time.Second

// gitPollInterval is the workspace git branch/status refresh interval.
const gitPollInterval = 3 * time.Second

// defaultPreviewLoadingDelay is how long a new preview may take before "Loading preview…" replaces the last frame.
const defaultPreviewLoadingDelay = 500 * time.Millisecond

type bootMsg struct{}

// previewLoadedMsg carries one finished read: a live pane (paneID) or a directory listing (dir).
type previewLoadedMsg struct {
	seq      uint64
	targetID string
	paneID   string
	dir      string
	text     string
	err      error
}

// previewLoadingMsg fires when a selection-change preview is still in flight after the loading delay.
type previewLoadingMsg struct{ seq uint64 }

// previewTickMsg re-reads the selected live pane once the previous read finished.
type previewTickMsg struct{ seq uint64 }

type snapshotTickMsg struct{}

type gitTickMsg struct{}

// snapshotLoadedMsg carries one live refresh. liveErr means the snapshot could not be read at all.
type snapshotLoadedMsg struct {
	seq uint64
	liveState
	liveErr error
}

// catalogLoadedMsg carries the sidebar layout and reusable-space definitions loaded after first paint.
type catalogLoadedMsg struct {
	layout      sidebarLayout
	definitions []space.Definition
	errs        []string
}

type acceptedMsg struct{}

type errorMsg struct{ err error }

type cancelSet struct {
	mu       sync.Mutex
	preview  context.CancelFunc
	snapshot context.CancelFunc
	accept   context.CancelFunc
	git      context.CancelFunc
	catalog  context.CancelFunc
}

func (c *cancelSet) replace(slot *context.CancelFunc) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if *slot != nil {
		(*slot)()
	}
	ctx, cancel := context.WithCancel(context.Background())
	*slot = cancel
	return ctx
}

func (c *cancelSet) cancelPreview() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.preview != nil {
		c.preview()
		c.preview = nil
	}
}

func (c *cancelSet) cancelAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, slot := range []*context.CancelFunc{&c.preview, &c.snapshot, &c.accept, &c.git, &c.catalog} {
		if *slot != nil {
			(*slot)()
			*slot = nil
		}
	}
}

func (m *model) io() *cancelSet {
	if m.cancels == nil {
		m.cancels = &cancelSet{}
	}
	return m.cancels
}

type model struct {
	view               string
	query              string
	searching          bool
	allItems           []Item
	visible            []Item
	searchScratch      []string
	selectedID         string
	snapshot           herdr.SessionSnapshot
	history            focus.History
	layout             sidebarLayout
	definitions        []space.Definition
	associationRecords []space.AssociationRecord
	unresolvedRecords  []space.AssociationRecord
	catalogErrors      []string
	width              int
	height             int
	theme              colorTheme

	// Preview state. previewText is the last frame shown; it survives selection
	// changes until the new read lands so navigation never blanks the pane.
	// previewListing marks it as a directory listing rather than a live pane frame.
	previewText         string
	previewListing      bool
	previewErr          string
	previewPane         string
	previewDir          string
	previewLoading      bool
	previewInFlight     bool
	previewSeq          uint64
	previewEvery        time.Duration
	previewLoadingDelay time.Duration
	readPane            func(ctx context.Context, paneID string) (string, error)
	readDir             func(ctx context.Context, dir string) (string, error)

	// Catalog state. snapshotReady flips on the first live snapshot; catalogReady
	// on the first layout/definition load. Until then the list shows loading copy.
	snapshotReady    bool
	catalogReady     bool
	liveErr          string
	assocErr         string
	acceptErr        string
	statusErr        string
	snapshotSeq      uint64
	snapshotInFlight bool
	gitInFlight      bool
	gitByDirectory   map[string]gitinfo.WorkspaceGit

	widePreviewMinCols int
	splitListWidth     int
	dividerDrag        bool
	mouseOut           io.Writer

	launch        launchContext
	pendingAccept bool
	quitting      bool
	cancels       *cancelSet
	milestones    map[string]bool
}

// newAsyncModel paints chrome first; snapshot, history, layout and definitions load after Init.
func newAsyncModel(view string, theme colorTheme, pollEvery time.Duration, wideMin int, configErrs []string) model {
	m := model{
		view:               view,
		theme:              theme,
		layout:             defaultSidebarLayout(),
		previewEvery:       pollEvery,
		widePreviewMinCols: wideMin,
		catalogErrors:      configErrs,
	}
	m.recomputeStatus()
	return m
}

// recomputeStatus picks the footer error: a failed accept, then live socket, then association, then config.
func (m *model) recomputeStatus() {
	switch {
	case m.acceptErr != "":
		m.statusErr = m.acceptErr
	case m.liveErr != "":
		m.statusErr = m.liveErr
	case m.assocErr != "":
		m.statusErr = m.assocErr
	case len(m.catalogErrors) > 0:
		m.statusErr = strings.Join(m.catalogErrors, "; ")
	default:
		m.statusErr = ""
	}
}

func (m *model) rebuildVisible() {
	items := m.catalogItems()
	m.allItems = items
	m.visible, m.searchScratch = filterItemsInto(m.visible[:0], m.searchScratch, items, m.query)
	m.selectedID = preselectItemID(m.view, m.visible, m.history, m.launch)
	// previewPane and previewDir stay unset here so the first afterSelectionChange
	// treats the preselected item as a selection change: a hedged read with the
	// loading indicator, not an unhedged refresh that can sit on Herdr's 100ms tick.
}

func (m *model) catalogItems() []Item {
	live := liveState{snapshot: m.snapshot, history: m.history, records: m.associationRecords, unresolved: m.unresolvedRecords}
	return assembleItems(m.view, live, m.layout, m.definitions, m.gitByDirectory)
}

// previewTarget names what the item previews: a live agent pane, or the directory of a space/template.
func previewTarget(item Item) (paneID, dir string) {
	if item.Kind == KindAgent {
		return item.PaneID, ""
	}
	return "", item.Path
}

func (m model) previewTargetMatches(item Item) bool {
	paneID, dir := previewTarget(item)
	return paneID == m.previewPane && dir == m.previewDir
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg { return bootMsg{} }
}

func tickSnapshot() tea.Cmd {
	return tea.Tick(snapshotPollInterval, func(time.Time) tea.Msg { return snapshotTickMsg{} })
}

func tickGit() tea.Cmd {
	return tea.Tick(gitPollInterval, func(time.Time) tea.Msg { return gitTickMsg{} })
}

func (m model) tickPreview(seq uint64) tea.Cmd {
	every := m.previewEvery
	if every <= 0 {
		every = time.Duration(config.DefaultPreviewPollMilliseconds) * time.Millisecond
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return previewTickMsg{seq: seq} })
}

// showsPreview reports whether any preview pane (side or stacked) is on screen.
func (m model) showsPreview() bool {
	return m.frame().mode != previewHidden
}

func (m model) selectedItem() (Item, bool) {
	if m.selectedID == "" {
		return Item{}, false
	}
	for _, item := range m.visible {
		if item.ID == m.selectedID {
			return item, true
		}
	}
	return Item{}, false
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !trace.Enabled() {
		return m, update(&m, msg)
	}
	span := trace.Span("update", "msg", fmt.Sprintf("%T", msg))
	cmd := update(&m, msg)
	span()
	return m, cmd
}

// traceMilestone logs the first occurrence of a named startup milestone.
func (m *model) traceMilestone(name string) {
	if m.milestones == nil {
		m.milestones = map[string]bool{}
	}
	if m.milestones[name] {
		return
	}
	m.milestones[name] = true
	trace.Event("milestone."+name, "items", len(m.visible))
}

func update(m *model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case bootMsg:
		if m.quitting {
			return nil
		}
		var cmds []tea.Cmd
		if !m.catalogReady {
			cmds = append(cmds, m.startCatalog())
		}
		cmds = append(cmds, m.startSnapshot(), tickSnapshot(), tickGit())
		if m.snapshotReady {
			cmds = append(cmds, m.startGit(), m.afterSelectionChange())
		}
		return tea.Batch(cmds...)
	case tea.WindowSizeMsg:
		wasShown := m.showsPreview()
		m.width = msg.Width
		m.height = msg.Height
		m.traceMilestone("window_size")
		nowShown := m.showsPreview()
		if !nowShown {
			// Keep the last frame for an instant repaint when the popup grows again.
			m.stopPreviewChain()
			return nil
		}
		if !wasShown {
			return m.startPreview(false)
		}
		return nil
	case snapshotTickMsg:
		if m.quitting {
			return nil
		}
		return tea.Batch(tickSnapshot(), m.startSnapshot())
	case gitTickMsg:
		if m.quitting {
			return nil
		}
		return tea.Batch(tickGit(), m.startGit())
	case previewTickMsg:
		if m.quitting || msg.seq != m.previewSeq || m.previewInFlight {
			return nil
		}
		return m.startPreview(true)
	case previewLoadingMsg:
		if msg.seq == m.previewSeq && m.previewInFlight {
			m.previewLoading = true
		}
		return nil
	case catalogLoadedMsg:
		if m.quitting {
			return nil
		}
		m.layout = msg.layout
		m.definitions = msg.definitions
		m.catalogErrors = append(append([]string{}, m.catalogErrors...), msg.errs...)
		m.catalogReady = true
		m.traceMilestone("catalog_loaded")
		m.recomputeStatus()
		if m.snapshotReady {
			// Template directories need their own git pass; a pass already running picks them up on the next tick.
			return tea.Batch(m.refreshMembership(), m.startGit())
		}
		return nil
	case gitLoadedMsg:
		m.gitInFlight = false
		if m.quitting {
			return nil
		}
		m.gitByDirectory = msg.byDirectory
		if !m.snapshotReady {
			return nil
		}
		return m.refreshMembership()
	case snapshotLoadedMsg:
		if m.quitting || msg.seq != m.snapshotSeq {
			return nil
		}
		m.snapshotInFlight = false
		if msg.liveErr != nil {
			m.liveErr = msg.liveErr.Error()
			m.recomputeStatus()
			return nil
		}
		m.liveErr = ""
		m.snapshot = msg.snapshot
		m.history = msg.history
		m.associationRecords = msg.records
		m.unresolvedRecords = msg.unresolved
		m.assocErr = ""
		if msg.assocErr != nil {
			m.assocErr = msg.assocErr.Error()
		}
		m.recomputeStatus()
		if !m.snapshotReady {
			m.snapshotReady = true
			m.launch = launchFromSnapshot(m.snapshot, m.history)
			m.rebuildVisible()
			m.traceMilestone("snapshot_applied")
			return tea.Batch(m.afterSelectionChange(), m.startGit())
		}
		return m.refreshMembership()
	case previewLoadedMsg:
		if msg.seq != m.previewSeq {
			trace.Event("preview.stale", "pane", msg.paneID)
			return nil
		}
		m.previewInFlight = false
		m.previewLoading = false
		if m.quitting {
			return nil
		}
		item, ok := m.selectedItem()
		if !ok || msg.targetID != m.selectedID || !m.showsPreview() {
			return nil
		}
		if paneID, dir := previewTarget(item); msg.paneID != paneID || msg.dir != dir {
			return nil
		}
		if msg.err != nil {
			m.previewErr = msg.err.Error()
			m.previewText = ""
			m.previewListing = false
		} else {
			m.previewErr = ""
			m.previewText = msg.text
			m.previewListing = msg.dir != ""
			m.traceMilestone("preview_painted")
		}
		if msg.dir != "" {
			// Directory listings are read once per selection; only live panes poll.
			return nil
		}
		return m.tickPreview(m.previewSeq)
	case acceptedMsg:
		m.quitting = true
		m.io().cancelAll()
		return tea.Quit
	case errorMsg:
		m.pendingAccept = false
		m.acceptErr = msg.err.Error()
		m.recomputeStatus()
		return nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			if msg.Type == tea.KeyEsc && m.searching {
				m.searching = false
				m.query = ""
				m.selectedID = ""
				m.applyQuery()
				return m.afterSelectionChange()
			}
			m.quitting = true
			m.pendingAccept = false
			m.io().cancelAll()
			return tea.Quit
		case tea.KeyEnter:
			return m.startAccept()
		case tea.KeyUp:
			// Ranked blocks grow upward, so later indexes sit physically higher.
			m.moveSelection(1)
			return m.afterSelectionChange()
		case tea.KeyDown:
			m.moveSelection(-1)
			return m.afterSelectionChange()
		case tea.KeyTab:
			m.cycleView(1)
			return m.afterSelectionChange()
		case tea.KeyShiftTab:
			m.cycleView(-1)
			return m.afterSelectionChange()
		case tea.KeyBackspace:
			if m.searching {
				if len(m.query) > 0 {
					r := []rune(m.query)
					m.query = string(r[:len(r)-1])
					m.applyQuery()
				} else {
					m.searching = false
				}
				return m.afterSelectionChange()
			}
		default:
			if msg.Type == tea.KeyRunes {
				runes := termtext.StripControls(string(msg.Runes))
				if !m.searching {
					if runes == "/" {
						m.searching = true
					}
					break
				}
				m.query += runes
				m.applyQuery()
				return m.afterSelectionChange()
			}
		}
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return nil
}

func (m *model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	fr := m.frame()
	if m.dividerDrag {
		switch msg.Action {
		case tea.MouseActionMotion, tea.MouseActionPress:
			if msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonNone {
				m.setSplitFromMouse(msg.X)
				return nil
			}
			m.dividerDrag = false
			return m.setMouseCellMotion(false)
		default:
			m.dividerDrag = false
			return m.setMouseCellMotion(false)
		}
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	if fr.mode == previewSide && msg.X == fr.dividerX && msg.Y >= fr.previewY && msg.Y < fr.previewY+fr.previewH {
		m.dividerDrag = true
		return m.setMouseCellMotion(true)
	}
	id := m.itemIDAtMouse(msg.X, msg.Y)
	if id == "" || id == m.selectedID {
		return nil
	}
	m.selectedID = id
	return m.afterSelectionChange()
}

func (m *model) setSplitFromMouse(x int) {
	lo, hi := m.splitListBounds()
	if hi < lo {
		return
	}
	if x < lo {
		x = lo
	}
	if x > hi {
		x = hi
	}
	m.splitListWidth = x
}

// setMouseCellMotion toggles button-motion mouse reporting (DECSET 1002) around a divider drag.
// Ordinary use keeps click-only reporting, enabled by Run.
func (m *model) setMouseCellMotion(on bool) tea.Cmd {
	out := m.mouseOut
	if out == nil {
		return nil
	}
	seq := "\x1b[?1002l"
	if on {
		seq = "\x1b[?1002h"
	}
	return func() tea.Msg {
		_, _ = io.WriteString(out, seq)
		return nil
	}
}

func (m *model) startCatalog() tea.Cmd {
	if m.quitting {
		return nil
	}
	ctx := m.io().replace(&m.io().catalog)
	return func() tea.Msg {
		defer trace.Span("catalog.load")()
		if ctx.Err() != nil {
			return nil
		}
		layout, definitions, errs := loadCatalog()
		if ctx.Err() != nil {
			return nil
		}
		return catalogLoadedMsg{layout: layout, definitions: definitions, errs: errs}
	}
}

func (m *model) startSnapshot() tea.Cmd {
	if m.quitting || m.snapshotInFlight {
		return nil
	}
	m.snapshotInFlight = true
	m.snapshotSeq++
	seq := m.snapshotSeq
	ctx := m.io().replace(&m.io().snapshot)
	if !m.snapshotReady {
		// The first snapshot is what the user waits for; later polls are not.
		ctx = herdr.WithHedge(ctx)
	}
	return func() tea.Msg {
		defer trace.Span("snapshot.load")()
		live, err := loadLive(ctx)
		return snapshotLoadedMsg{seq: seq, liveState: live, liveErr: err}
	}
}

func (m *model) paneReader() func(ctx context.Context, paneID string) (string, error) {
	if m.readPane != nil {
		return m.readPane
	}
	return func(ctx context.Context, paneID string) (string, error) {
		span := trace.Span("preview.read", "pane", paneID)
		result, _, err := herdr.ReadPaneVisibleANSIContext(ctx, paneID)
		if err != nil {
			span("err", err)
			return "", err
		}
		filterSpan := trace.Span("preview.filter", "pane", paneID)
		text := termtext.KeepSGR(result.Text)
		filterSpan("raw", len(result.Text), "filtered", len(text))
		span("raw", len(result.Text), "revision", result.Revision)
		return text, nil
	}
}

// stopPreviewChain cancels the in-flight read and invalidates pending ticks and replies.
func (m *model) stopPreviewChain() {
	m.io().cancelPreview()
	m.previewInFlight = false
	m.previewLoading = false
	m.previewSeq++
}

func (m *model) clearPreview() {
	m.previewText = ""
	m.previewListing = false
	m.previewErr = ""
	m.previewPane = ""
	m.previewDir = ""
	m.previewLoading = false
}

// startPreview reads the selected live pane. refresh=true keeps the current frame with no
// loading indicator (same pane, next poll); refresh=false is a selection change: the last
// frame stays visible until the read lands or the loading delay passes.
func (m *model) startPreview(refresh bool) tea.Cmd {
	if m.quitting || !m.showsPreview() {
		return nil
	}
	item, ok := m.selectedItem()
	if !ok {
		m.stopPreviewChain()
		m.clearPreview()
		return nil
	}
	paneID, dir := previewTarget(item)
	if paneID == "" && dir == "" {
		m.stopPreviewChain()
		m.clearPreview()
		return nil
	}
	if m.previewInFlight && m.previewTargetMatches(item) {
		return nil
	}
	if dir != "" {
		return m.startDirectoryPreview(item, dir)
	}
	m.previewInFlight = true
	m.previewSeq++
	seq := m.previewSeq
	targetID := item.ID
	m.previewPane = paneID
	m.previewDir = ""
	if !refresh {
		m.previewErr = ""
		m.previewLoading = false
	}
	ctx := m.io().replace(&m.io().preview)
	if !refresh {
		ctx = herdr.WithHedge(ctx)
	}
	read := m.paneReader()
	readCmd := func() tea.Msg {
		text, err := read(ctx, paneID)
		if err != nil {
			return previewLoadedMsg{seq: seq, targetID: targetID, paneID: paneID, err: err}
		}
		return previewLoadedMsg{seq: seq, targetID: targetID, paneID: paneID, text: text}
	}
	if refresh {
		return readCmd
	}
	return tea.Batch(readCmd, m.previewLoadingCmd(ctx, seq))
}

// previewLoadingCmd fires previewLoadingMsg once the loading delay passes, unless the read finishes (cancels ctx) first.
func (m *model) previewLoadingCmd(ctx context.Context, seq uint64) tea.Cmd {
	delay := m.previewLoadingDelay
	if delay <= 0 {
		delay = defaultPreviewLoadingDelay
	}
	return func() tea.Msg {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return previewLoadingMsg{seq: seq}
		}
	}
}

// startDirectoryPreview lists the selected row's directory once. It never polls and never
// touches Herdr. Recovery hints (PreviewText) sit above the listing when present.
func (m *model) startDirectoryPreview(item Item, dir string) tea.Cmd {
	m.stopPreviewChain()
	m.previewInFlight = true
	seq := m.previewSeq
	targetID := item.ID
	header := item.PreviewText
	m.previewPane = ""
	m.previewDir = dir
	m.previewErr = ""
	m.previewLoading = false
	ctx := m.io().replace(&m.io().preview)
	read := m.readDir
	if read == nil {
		read = dirlist.Read
	}
	readCmd := func() tea.Msg {
		text, err := read(ctx, dir)
		if err != nil {
			return previewLoadedMsg{seq: seq, targetID: targetID, dir: dir, err: err}
		}
		if header != "" {
			text = header + "\n\n" + text
		}
		return previewLoadedMsg{seq: seq, targetID: targetID, dir: dir, text: text}
	}
	return tea.Batch(readCmd, m.previewLoadingCmd(ctx, seq))
}

func (m *model) startAccept() tea.Cmd {
	if m.quitting || m.pendingAccept {
		return nil
	}
	item, ok := m.selectedItem()
	if !ok {
		return nil
	}
	// Escape can cancel this only before Herdr accepts the focus write.
	// A request already accepted cannot be undone by closing the socket.
	m.pendingAccept = true
	selectedID := item.ID
	kind := item.Kind
	paneID := item.PaneID
	workspaceID := item.WorkspaceID
	definitionID := item.DefinitionID
	ctx := m.io().replace(&m.io().accept)
	return func() tea.Msg {
		defer trace.Span("accept", "kind", kind)()
		if ctx.Err() != nil {
			return errorMsg{err: ctx.Err()}
		}
		if kind == KindDefinition {
			_, err := space.Open(ctx, definitionID)
			if ctx.Err() != nil {
				return errorMsg{err: ctx.Err()}
			}
			if err != nil {
				return errorMsg{err: err}
			}
			return acceptedMsg{}
		}
		snapshot, witness, err := herdr.LoadSessionSnapshotContext(ctx)
		if err != nil {
			return errorMsg{err: err}
		}
		history, err := focus.LoadPruned(snapshot, witness)
		if err != nil {
			return errorMsg{err: err}
		}
		if ctx.Err() != nil {
			return errorMsg{err: ctx.Err()}
		}
		if kind == KindAgent {
			live, ok := focus.CurrentAgentLiveID(history, paneID)
			if !ok || selectionID(KindAgent, live.String()) != selectedID {
				return errorMsg{err: fmt.Errorf("hseh focus: selected occupant was replaced")}
			}
			tabID := ""
			for _, agent := range snapshot.Agents {
				if agent.PaneID == paneID {
					tabID = agent.TabID
					break
				}
			}
			err = herdr.FocusAgentContext(ctx, tabID)
		} else {
			err = herdr.FocusWorkspaceContext(ctx, workspaceID)
		}
		if ctx.Err() != nil {
			return errorMsg{err: ctx.Err()}
		}
		if err != nil {
			return errorMsg{err: err}
		}
		return acceptedMsg{}
	}
}

// afterSelectionChange redirects the preview clock to the selected target. The last
// frame stays on screen; a different pane starts a selection-change read.
func (m *model) afterSelectionChange() tea.Cmd {
	item, ok := m.selectedItem()
	if !ok {
		m.stopPreviewChain()
		m.clearPreview()
		return nil
	}
	paneID, dir := previewTarget(item)
	if !m.previewTargetMatches(item) || (paneID == "" && dir == "") {
		return m.startPreview(false)
	}
	if m.previewInFlight || dir != "" {
		// Same pane still reading, or the same directory already listed: nothing to do.
		return nil
	}
	return m.startPreview(true)
}

func (m *model) applyQuery() {
	m.visible, m.searchScratch = filterItemsInto(m.visible[:0], m.searchScratch, m.allItems, m.query)
	if m.query != "" {
		m.selectedID = ""
		if len(m.visible) > 0 {
			m.selectedID = m.visible[0].ID
		}
		return
	}
	if !hasItemID(m.visible, m.selectedID) {
		m.selectedID = preselectItemID(m.view, m.visible, m.history, m.launch)
	}
}

// mergePreservingOrder keeps prev's order for rows still in next, then appends next's new rows.
func mergePreservingOrder(prev, next []Item) []Item {
	byID := make(map[string]Item, len(next))
	for _, item := range next {
		byID[item.ID] = item
	}
	merged := make([]Item, 0, len(next))
	seen := make(map[string]bool, len(next))
	for _, item := range prev {
		if fresh, ok := byID[item.ID]; ok {
			merged = append(merged, fresh)
			seen[item.ID] = true
		}
	}
	for _, item := range next {
		if !seen[item.ID] {
			merged = append(merged, item)
		}
	}
	return merged
}

// refreshMembership merges fresh catalog rows without reordering rows that merely changed status.
func (m *model) refreshMembership() tea.Cmd {
	fresh := m.catalogItems()
	if m.query == "" {
		kept := mergePreservingOrder(m.allItems, fresh)
		m.allItems = kept
		m.visible = append(m.visible[:0], kept...)
	} else {
		var matched []Item
		matched, m.searchScratch = filterItemsInto(nil, m.searchScratch, fresh, m.query)
		m.allItems = fresh
		m.visible = mergePreservingOrder(m.visible, matched)
	}
	item, ok := m.selectedItem()
	if !ok {
		m.selectedID = ""
		m.stopPreviewChain()
		m.clearPreview()
		return nil
	}
	if !m.previewTargetMatches(item) {
		return m.afterSelectionChange()
	}
	return nil
}

func (m *model) moveSelection(delta int) {
	if len(m.visible) == 0 {
		m.selectedID = ""
		return
	}
	index := 0
	for i, item := range m.visible {
		if item.ID == m.selectedID {
			index = i
			break
		}
	}
	index += delta
	if index < 0 {
		index = 0
	}
	if index >= len(m.visible) {
		index = len(m.visible) - 1
	}
	m.selectedID = m.visible[index].ID
}

func (m *model) cycleView(delta int) {
	order := []string{ViewSpaces, ViewAgents}
	index := 0
	for i, view := range order {
		if view == m.view {
			index = i
			break
		}
	}
	index = (index + delta) % len(order)
	if index < 0 {
		index += len(order)
	}
	m.view = order[index]
	m.allItems = m.catalogItems()
	m.applyQuery()
}

// Click-only mouse reporting: normal tracking (1000) with SGR encoding (1006).
// Bubble Tea v1 offers only cell/all-motion modes, so hseh manages this mode itself.
const (
	mouseClickOnlyEnable  = "\x1b[?1000h\x1b[?1006h"
	mouseClickOnlyDisable = "\x1b[?1002l\x1b[?1006l\x1b[?1000l"
)

// Run opens the interactive picker on the given view and blocks until it exits.
func Run(view string) error {
	view, err := ParseView(view)
	if err != nil {
		return err
	}
	settings, configErrs := config.Load()
	theme, themeErrs := loadTheme()
	configErrs = append(configErrs, themeErrs...)
	m := newAsyncModel(view, theme, settings.PreviewPoll, settings.WidePreviewMinColumns, configErrs)
	m.mouseOut = os.Stdout
	defer m.io().cancelAll()
	_, _ = io.WriteString(os.Stdout, mouseClickOnlyEnable)
	defer func() { _, _ = io.WriteString(os.Stdout, mouseClickOnlyDisable) }()
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
