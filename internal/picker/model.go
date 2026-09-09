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
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
	"github.com/regutierrez/hseh/internal/termtext"
	"github.com/regutierrez/hseh/internal/trace"
)

// DefaultSnapshotPollMilliseconds is the live membership/status refresh interval.
// It is slower than the preview clock: membership changes rarely, terminal output often.
const DefaultSnapshotPollMilliseconds = 1000

// DefaultGitPollMilliseconds is the workspace git branch/status refresh interval.
const DefaultGitPollMilliseconds = 3000

// DefaultPreviewLoadingDelay is how long a new preview may take before "Loading preview…" replaces the last frame.
const DefaultPreviewLoadingDelay = 500 * time.Millisecond

type bootMsg struct{}

type previewLoadedMsg struct {
	seq      uint64
	targetID string
	paneID   string
	text     string
	err      error
}

// previewLoadingMsg fires when a selection-change preview is still in flight after the loading delay.
type previewLoadingMsg struct{ seq uint64 }

// previewTickMsg re-reads the selected live pane once the previous read finished.
type previewTickMsg struct{ seq uint64 }

type snapshotTickMsg struct{}

type gitTickMsg struct{}

type snapshotLoadedMsg struct {
	seq        uint64
	snapshot   herdr.SessionSnapshot
	history    focus.History
	records    []space.AssociationRecord
	unresolved []space.AssociationRecord
	liveErr    error
	catalogErr error
}

// catalogLoadedMsg carries the sidebar layout and reusable-space definitions loaded after first paint.
type catalogLoadedMsg struct {
	layout      SidebarLayout
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
	if c == nil {
		return context.Background()
	}
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
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.preview != nil {
		c.preview()
		c.preview = nil
	}
}

func (c *cancelSet) cancelAll() {
	if c == nil {
		return
	}
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
	allItems           []Item
	visible            []Item
	searchScratch      []string
	selectedID         string
	snapshot           herdr.SessionSnapshot
	history            focus.History
	layout             SidebarLayout
	definitions        []space.Definition
	associationRecords []space.AssociationRecord
	unresolvedRecords  []space.AssociationRecord
	catalogErrors      []string
	width              int
	height             int
	theme              colorTheme

	// Preview state. previewText is the last frame shown; it survives selection
	// changes until the new read lands so navigation never blanks the pane.
	previewText         string
	previewTextLive     bool
	previewErr          string
	previewPane         string
	previewLoading      bool
	previewInFlight     bool
	previewSeq          uint64
	previewEvery        time.Duration
	previewLoadingDelay time.Duration
	readPane            func(ctx context.Context, paneID string) (string, error)

	// Catalog state. snapshotReady flips on the first live snapshot; catalogReady
	// on the first layout/definition load. Until then the list shows loading copy.
	snapshotReady    bool
	catalogReady     bool
	liveErr          string
	assocErr         string
	acceptErr        string
	statusErr        string
	snapshotEvery    time.Duration
	gitEvery         time.Duration
	snapshotSeq      uint64
	snapshotInFlight bool
	gitInFlight      bool
	gitByDirectory   map[string]gitinfo.WorkspaceGit

	widePreviewMinCols int
	splitListWidth     int
	dividerDrag        bool
	mouseOut           io.Writer

	launch        LaunchContext
	pendingAccept bool
	quitting      bool
	cancels       *cancelSet
	milestones    map[string]bool
}

// newModel builds a model with the catalog already loaded (used by tests and list tooling).
func newModel(view string, snapshot herdr.SessionSnapshot, history focus.History, layout SidebarLayout, pollEvery time.Duration, wideMin int) model {
	history = focus.Prune(history, snapshot)
	launch := launchFromEnv(snapshot, history)
	m := model{
		view:               view,
		snapshot:           snapshot,
		history:            history,
		layout:             layout,
		previewEvery:       pollEvery,
		widePreviewMinCols: wideMin,
		launch:             launch,
		cancels:            &cancelSet{},
		snapshotReady:      true,
		catalogReady:       true,
	}
	m.rebuildVisible()
	return m
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
		cancels:            &cancelSet{},
	}
	m.recomputeStatus()
	return m
}

func (m *model) setSpaceCatalog(definitions []space.Definition, records, unresolved []space.AssociationRecord, errs []string) {
	m.definitions = definitions
	m.associationRecords = records
	m.unresolvedRecords = unresolved
	m.catalogErrors = errs
	m.catalogReady = true
	m.recomputeStatus()
	m.rebuildVisible()
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
	m.selectedID = PreselectItemID(m.view, m.visible, m.history, m.launch)
	if item, ok := m.selectedItem(); ok {
		m.previewPane = item.PreviewPane
		if item.Kind == KindDefinition {
			m.previewText = item.PreviewText
			m.previewTextLive = false
		}
	}
}

func (m *model) catalogItems() []Item {
	snapshot := m.snapshot
	snapshot.GitByDirectory = m.gitByDirectory
	items := BuildItemsWithLayout(m.view, snapshot, m.history, m.layout)
	return AppendUnopenedDefinitionItems(items, m.view, m.snapshot, m.definitions, m.associationRecords, m.unresolvedRecords)
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg { return bootMsg{} }
}

func tickAfter(every, fallback time.Duration, msg tea.Msg) tea.Cmd {
	if every <= 0 {
		every = fallback
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return msg })
}

func (m model) tickSnapshot() tea.Cmd {
	return tickAfter(m.snapshotEvery, time.Duration(DefaultSnapshotPollMilliseconds)*time.Millisecond, snapshotTickMsg{})
}

func (m model) tickGit() tea.Cmd {
	return tickAfter(m.gitEvery, time.Duration(DefaultGitPollMilliseconds)*time.Millisecond, gitTickMsg{})
}

func (m model) tickPreview(seq uint64) tea.Cmd {
	return tickAfter(m.previewEvery, time.Duration(config.DefaultPreviewPollMilliseconds)*time.Millisecond, previewTickMsg{seq: seq})
}

// wideEnoughForPreview reports side-by-side layout. Narrower popups stack the preview instead of hiding it.
func (m model) wideEnoughForPreview() bool {
	return m.frame().mode == previewSide
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
		cmds = append(cmds, m.startSnapshot(), m.tickSnapshot(), m.tickGit())
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
		return tea.Batch(m.tickSnapshot(), m.startSnapshot())
	case gitTickMsg:
		if m.quitting {
			return nil
		}
		return tea.Batch(m.tickGit(), m.startGit())
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
			return m.refreshMembership()
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
		if msg.catalogErr != nil {
			m.associationRecords = nil
			m.unresolvedRecords = nil
			m.assocErr = msg.catalogErr.Error()
		} else {
			m.associationRecords = msg.records
			m.unresolvedRecords = msg.unresolved
			m.assocErr = ""
		}
		m.recomputeStatus()
		if !m.snapshotReady {
			m.snapshotReady = true
			m.launch = launchFromEnv(m.snapshot, m.history)
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
		if !ok || msg.targetID != m.selectedID || msg.paneID != item.PreviewPane || !m.showsPreview() {
			return nil
		}
		if msg.err != nil {
			m.previewErr = msg.err.Error()
			m.previewText = ""
			m.previewTextLive = false
		} else {
			m.previewErr = ""
			m.previewText = msg.text
			m.previewTextLive = true
			m.traceMilestone("preview_painted")
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
			m.quitting = true
			m.pendingAccept = false
			m.io().cancelAll()
			m.previewSeq++
			m.snapshotSeq++
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
			if len(m.query) > 0 {
				r := []rune(m.query)
				m.query = string(r[:len(r)-1])
				m.applyQuery()
			}
			return m.afterSelectionChange()
		default:
			if msg.Type == tea.KeyRunes {
				m.query += termtext.StripControls(string(msg.Runes))
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
		layout, errs := LoadSidebarLayout("")
		definitions, defErrs := space.LoadDefinitions(config.SpaceDefinitionsDir())
		errs = append(errs, defErrs...)
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
		snapshot, witness, err := herdr.LoadSessionSnapshotContext(ctx)
		if err != nil {
			return snapshotLoadedMsg{seq: seq, liveErr: err}
		}
		history, err := focus.LoadPruned(snapshot, witness)
		if err != nil {
			return snapshotLoadedMsg{seq: seq, liveErr: err}
		}
		state, assocErr := space.LoadAssociationFile(config.StateDir())
		if assocErr != nil {
			return snapshotLoadedMsg{seq: seq, snapshot: snapshot, history: history, catalogErr: assocErr}
		}
		state = space.ReconcileAssociationState(state, witness)
		return snapshotLoadedMsg{seq: seq, snapshot: snapshot, history: history, records: state.Records, unresolved: state.Unresolved}
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
	m.previewTextLive = false
	m.previewErr = ""
	m.previewPane = ""
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
	if item.Kind == KindDefinition {
		m.stopPreviewChain()
		m.previewPane = ""
		m.previewErr = ""
		m.previewText = item.PreviewText
		m.previewTextLive = false
		return nil
	}
	if item.PreviewPane == "" {
		m.stopPreviewChain()
		m.clearPreview()
		return nil
	}
	if m.previewInFlight && item.PreviewPane == m.previewPane {
		return nil
	}
	m.previewInFlight = true
	m.previewSeq++
	seq := m.previewSeq
	targetID := item.ID
	paneID := item.PreviewPane
	m.previewPane = paneID
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
	delay := m.previewLoadingDelay
	if delay <= 0 {
		delay = DefaultPreviewLoadingDelay
	}
	loadingCmd := func() tea.Msg {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			return previewLoadingMsg{seq: seq}
		}
	}
	return tea.Batch(readCmd, loadingCmd)
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
			if !ok || SelectionID(KindAgent, live.String()) != selectedID {
				return errorMsg{err: fmt.Errorf("hseh focus: selected occupant was replaced")}
			}
			err = herdr.FocusAgentContext(ctx, paneID)
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
	if item.Kind == KindDefinition || item.PreviewPane == "" {
		return m.startPreview(false)
	}
	if item.PreviewPane == m.previewPane {
		if m.previewInFlight {
			return nil
		}
		return m.startPreview(true)
	}
	return m.startPreview(false)
}

func (m *model) applyQuery() {
	m.visible, m.searchScratch = filterItemsInto(m.visible[:0], m.searchScratch, m.allItems, m.query)
	if !HasItemID(m.visible, m.selectedID) {
		m.selectedID = ""
		if m.query == "" {
			m.selectedID = PreselectItemID(m.view, m.visible, m.history, m.launch)
		}
	}
}

// refreshMembership merges fresh catalog rows without reordering rows that merely changed status.
func (m *model) refreshMembership() tea.Cmd {
	fresh := m.catalogItems()
	byID := make(map[string]Item, len(fresh))
	for _, item := range fresh {
		byID[item.ID] = item
	}
	if m.query == "" {
		kept := make([]Item, 0, len(fresh))
		seen := make(map[string]bool, len(fresh))
		for _, item := range m.allItems {
			if next, ok := byID[item.ID]; ok {
				kept = append(kept, next)
				seen[item.ID] = true
			}
		}
		for _, item := range fresh {
			if !seen[item.ID] {
				kept = append(kept, item)
			}
		}
		m.allItems = kept
		m.visible = append(m.visible[:0], kept...)
	} else {
		var matched []Item
		matched, m.searchScratch = filterItemsInto(nil, m.searchScratch, fresh, m.query)
		matchedByID := make(map[string]Item, len(matched))
		for _, item := range matched {
			matchedByID[item.ID] = item
		}
		visible := make([]Item, 0, len(matched))
		seen := make(map[string]bool, len(matched))
		for _, item := range m.visible {
			if next, ok := matchedByID[item.ID]; ok {
				visible = append(visible, next)
				seen[item.ID] = true
			}
		}
		for _, item := range matched {
			if !seen[item.ID] {
				visible = append(visible, item)
			}
		}
		m.allItems = fresh
		m.visible = visible
	}
	item, ok := m.selectedItem()
	if !ok {
		m.selectedID = ""
		m.stopPreviewChain()
		m.clearPreview()
		return nil
	}
	if item.PreviewPane != m.previewPane {
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
	poll, configErrs := config.LoadPreviewPollInterval()
	wideMin, moreErrs := config.LoadWidePreviewMinColumns()
	configErrs = append(configErrs, moreErrs...)
	theme, themeErrs := loadTheme("")
	configErrs = append(configErrs, themeErrs...)
	m := newAsyncModel(view, theme, poll, wideMin, configErrs)
	m.mouseOut = os.Stdout
	defer m.io().cancelAll()
	_, _ = io.WriteString(os.Stdout, mouseClickOnlyEnable)
	defer func() { _, _ = io.WriteString(os.Stdout, mouseClickOnlyDisable) }()
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
