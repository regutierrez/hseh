package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// DefaultSnapshotPollMilliseconds is the live membership/status refresh interval.
// It is slower than the preview clock: membership changes rarely, terminal output often.
const DefaultSnapshotPollMilliseconds = 1000

// DefaultGitPollMilliseconds is the workspace git branch/status refresh interval.
const DefaultGitPollMilliseconds = 3000

// DefaultPreviewLoadingDelay is how long a new preview may take before "Loading preview…" replaces the last frame.
const DefaultPreviewLoadingDelay = 500 * time.Millisecond

type pickerBootMsg struct{}

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
	snapshot   HerdrSessionSnapshot
	history    FocusHistory
	records    []SpaceAssociationRecord
	unresolved []SpaceAssociationRecord
	liveErr    error
	catalogErr error
}

// catalogLoadedMsg carries the sidebar layout and reusable-space definitions loaded after first paint.
type catalogLoadedMsg struct {
	layout      SidebarLayout
	definitions []SpaceDefinition
	errs        []string
}

type pickerAcceptedMsg struct{}

type pickerErrorMsg struct{ err error }

type pickerCancels struct {
	mu       sync.Mutex
	preview  context.CancelFunc
	snapshot context.CancelFunc
	accept   context.CancelFunc
	git      context.CancelFunc
	catalog  context.CancelFunc
}

func (c *pickerCancels) replace(slot *context.CancelFunc) context.Context {
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

func (c *pickerCancels) cancelPreview() {
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

func (c *pickerCancels) cancelAll() {
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

func (m *pickerModel) io() *pickerCancels {
	if m.cancels == nil {
		m.cancels = &pickerCancels{}
	}
	return m.cancels
}

type pickerModel struct {
	view               string
	query              string
	allItems           []PickerItem
	visible            []PickerItem
	searchScratch      []string
	selectedID         string
	snapshot           HerdrSessionSnapshot
	history            FocusHistory
	layout             SidebarLayout
	definitions        []SpaceDefinition
	associationRecords []SpaceAssociationRecord
	unresolvedRecords  []SpaceAssociationRecord
	catalogErrors      []string
	width              int
	height             int
	theme              pickerTheme

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
	gitByDirectory   map[string]WorkspaceGit

	widePreviewMinCols int
	splitListWidth     int
	dividerDrag        bool
	mouseOut           io.Writer

	launch        pickerLaunchContext
	pendingAccept bool
	quitting      bool
	cancels       *pickerCancels
	milestones    map[string]bool
}

// newPickerModel builds a model with the catalog already loaded (used by tests and list tooling).
func newPickerModel(view string, snapshot HerdrSessionSnapshot, history FocusHistory, layout SidebarLayout, pollEvery time.Duration, wideMin int) pickerModel {
	history = pruneFocusHistory(history, snapshot)
	launch := pickerLaunchFromEnv(snapshot, history)
	model := pickerModel{
		view:               view,
		snapshot:           snapshot,
		history:            history,
		layout:             layout,
		previewEvery:       pollEvery,
		widePreviewMinCols: wideMin,
		launch:             launch,
		cancels:            &pickerCancels{},
		snapshotReady:      true,
		catalogReady:       true,
	}
	model.rebuildVisible()
	return model
}

// newAsyncPickerModel paints chrome first; snapshot, history, layout and definitions load after Init.
func newAsyncPickerModel(view string, theme pickerTheme, pollEvery time.Duration, wideMin int, configErrs []string) pickerModel {
	model := pickerModel{
		view:               view,
		theme:              theme,
		layout:             defaultSidebarLayout(),
		previewEvery:       pollEvery,
		widePreviewMinCols: wideMin,
		catalogErrors:      configErrs,
		cancels:            &pickerCancels{},
	}
	model.recomputeStatus()
	return model
}

func (m *pickerModel) setSpaceCatalog(definitions []SpaceDefinition, records, unresolved []SpaceAssociationRecord, errs []string) {
	m.definitions = definitions
	m.associationRecords = records
	m.unresolvedRecords = unresolved
	m.catalogErrors = errs
	m.catalogReady = true
	m.recomputeStatus()
	m.rebuildVisible()
}

// recomputeStatus picks the footer error: a failed accept, then live socket, then association, then config.
func (m *pickerModel) recomputeStatus() {
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

func (m *pickerModel) rebuildVisible() {
	items := m.catalogItems()
	m.allItems = items
	m.visible, m.searchScratch = filterPickerItemsInto(m.visible[:0], m.searchScratch, items, m.query)
	m.selectedID = PreselectPickerItemID(m.view, m.visible, m.history, m.launch)
	if item, ok := m.selectedItem(); ok {
		m.previewPane = item.PreviewPane
		if item.Kind == pickerKindDefinition {
			m.previewText = item.PreviewText
			m.previewTextLive = false
		}
	}
}

func (m *pickerModel) catalogItems() []PickerItem {
	snapshot := m.snapshot
	snapshot.GitByDirectory = m.gitByDirectory
	items := BuildPickerItemsWithLayout(m.view, snapshot, m.history, m.layout)
	return appendUnopenedDefinitionItems(items, m.view, m.snapshot, m.definitions, m.associationRecords, m.unresolvedRecords)
}

func (m pickerModel) Init() tea.Cmd {
	return func() tea.Msg { return pickerBootMsg{} }
}

func tickAfter(every, fallback time.Duration, msg tea.Msg) tea.Cmd {
	if every <= 0 {
		every = fallback
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return msg })
}

func (m pickerModel) tickSnapshot() tea.Cmd {
	return tickAfter(m.snapshotEvery, time.Duration(DefaultSnapshotPollMilliseconds)*time.Millisecond, snapshotTickMsg{})
}

func (m pickerModel) tickGit() tea.Cmd {
	return tickAfter(m.gitEvery, time.Duration(DefaultGitPollMilliseconds)*time.Millisecond, gitTickMsg{})
}

func (m pickerModel) tickPreview(seq uint64) tea.Cmd {
	return tickAfter(m.previewEvery, time.Duration(DefaultPreviewPollMilliseconds)*time.Millisecond, previewTickMsg{seq: seq})
}

// wideEnoughForPreview reports side-by-side layout. Narrower popups stack the preview instead of hiding it.
func (m pickerModel) wideEnoughForPreview() bool {
	return m.frame().mode == previewSide
}

// showsPreview reports whether any preview pane (side or stacked) is on screen.
func (m pickerModel) showsPreview() bool {
	return m.frame().mode != previewHidden
}

func (m pickerModel) selectedItem() (PickerItem, bool) {
	if m.selectedID == "" {
		return PickerItem{}, false
	}
	for _, item := range m.visible {
		if item.ID == m.selectedID {
			return item, true
		}
	}
	return PickerItem{}, false
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !traceEnabled() {
		return m, updatePicker(&m, msg)
	}
	span := traceSpan("update", "msg", fmt.Sprintf("%T", msg))
	cmd := updatePicker(&m, msg)
	span()
	return m, cmd
}

// traceMilestone logs the first occurrence of a named startup milestone.
func (m *pickerModel) traceMilestone(name string) {
	if m.milestones == nil {
		m.milestones = map[string]bool{}
	}
	if m.milestones[name] {
		return
	}
	m.milestones[name] = true
	traceEvent("milestone."+name, "items", len(m.visible))
}

func updatePicker(m *pickerModel, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pickerBootMsg:
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
	case pickerGitLoadedMsg:
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
			m.launch = pickerLaunchFromEnv(m.snapshot, m.history)
			m.rebuildVisible()
			m.traceMilestone("snapshot_applied")
			return tea.Batch(m.afterSelectionChange(), m.startGit())
		}
		return m.refreshMembership()
	case previewLoadedMsg:
		if msg.seq != m.previewSeq {
			traceEvent("preview.stale", "pane", msg.paneID)
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
	case pickerAcceptedMsg:
		m.quitting = true
		m.io().cancelAll()
		return tea.Quit
	case pickerErrorMsg:
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
				m.query += StripTerminalControls(string(msg.Runes))
				m.applyQuery()
				return m.afterSelectionChange()
			}
		}
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return nil
}

func (m *pickerModel) handleMouse(msg tea.MouseMsg) tea.Cmd {
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

func (m *pickerModel) setSplitFromMouse(x int) {
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
// Ordinary use keeps click-only reporting, enabled by runPicker.
func (m *pickerModel) setMouseCellMotion(on bool) tea.Cmd {
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

func (m *pickerModel) startCatalog() tea.Cmd {
	if m.quitting {
		return nil
	}
	ctx := m.io().replace(&m.io().catalog)
	return func() tea.Msg {
		defer traceSpan("catalog.load")()
		if ctx.Err() != nil {
			return nil
		}
		layout, errs := LoadSidebarLayout("")
		definitions, defErrs := LoadSpaceDefinitions(spaceDefinitionsDir())
		errs = append(errs, defErrs...)
		if ctx.Err() != nil {
			return nil
		}
		return catalogLoadedMsg{layout: layout, definitions: definitions, errs: errs}
	}
}

func (m *pickerModel) startSnapshot() tea.Cmd {
	if m.quitting || m.snapshotInFlight {
		return nil
	}
	m.snapshotInFlight = true
	m.snapshotSeq++
	seq := m.snapshotSeq
	ctx := m.io().replace(&m.io().snapshot)
	if !m.snapshotReady {
		// The first snapshot is what the user waits for; later polls are not.
		ctx = withHerdrHedge(ctx)
	}
	return func() tea.Msg {
		defer traceSpan("snapshot.load")()
		snapshot, witness, err := LoadHerdrSessionSnapshotContext(ctx)
		if err != nil {
			return snapshotLoadedMsg{seq: seq, liveErr: err}
		}
		history, err := LoadPrunedFocusHistory(snapshot, witness)
		if err != nil {
			return snapshotLoadedMsg{seq: seq, liveErr: err}
		}
		state, assocErr := loadSpaceAssociationFile(pluginStateDir())
		if assocErr != nil {
			return snapshotLoadedMsg{seq: seq, snapshot: snapshot, history: history, catalogErr: assocErr}
		}
		state = reconcileSpaceAssociationState(state, witness)
		return snapshotLoadedMsg{seq: seq, snapshot: snapshot, history: history, records: state.Records, unresolved: state.Unresolved}
	}
}

func (m *pickerModel) paneReader() func(ctx context.Context, paneID string) (string, error) {
	if m.readPane != nil {
		return m.readPane
	}
	return func(ctx context.Context, paneID string) (string, error) {
		span := traceSpan("preview.read", "pane", paneID)
		result, _, err := ReadHerdrPaneVisibleANSIContext(ctx, paneID)
		if err != nil {
			span("err", err)
			return "", err
		}
		filterSpan := traceSpan("preview.filter", "pane", paneID)
		text := AllowVisiblePreviewANSI(result.Text)
		filterSpan("raw", len(result.Text), "filtered", len(text))
		span("raw", len(result.Text), "revision", result.Revision)
		return text, nil
	}
}

// stopPreviewChain cancels the in-flight read and invalidates pending ticks and replies.
func (m *pickerModel) stopPreviewChain() {
	m.io().cancelPreview()
	m.previewInFlight = false
	m.previewLoading = false
	m.previewSeq++
}

func (m *pickerModel) clearPreview() {
	m.previewText = ""
	m.previewTextLive = false
	m.previewErr = ""
	m.previewPane = ""
	m.previewLoading = false
}

// startPreview reads the selected live pane. refresh=true keeps the current frame with no
// loading indicator (same pane, next poll); refresh=false is a selection change: the last
// frame stays visible until the read lands or the loading delay passes.
func (m *pickerModel) startPreview(refresh bool) tea.Cmd {
	if m.quitting || !m.showsPreview() {
		return nil
	}
	item, ok := m.selectedItem()
	if !ok {
		m.stopPreviewChain()
		m.clearPreview()
		return nil
	}
	if item.Kind == pickerKindDefinition {
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
		ctx = withHerdrHedge(ctx)
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

func (m *pickerModel) startAccept() tea.Cmd {
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
		defer traceSpan("accept", "kind", kind)()
		if ctx.Err() != nil {
			return pickerErrorMsg{err: ctx.Err()}
		}
		if kind == pickerKindDefinition {
			_, err := OpenReusableSpace(ctx, definitionID)
			if ctx.Err() != nil {
				return pickerErrorMsg{err: ctx.Err()}
			}
			if err != nil {
				return pickerErrorMsg{err: err}
			}
			return pickerAcceptedMsg{}
		}
		snapshot, witness, err := LoadHerdrSessionSnapshotContext(ctx)
		if err != nil {
			return pickerErrorMsg{err: err}
		}
		history, err := LoadPrunedFocusHistory(snapshot, witness)
		if err != nil {
			return pickerErrorMsg{err: err}
		}
		if ctx.Err() != nil {
			return pickerErrorMsg{err: ctx.Err()}
		}
		if kind == pickerKindAgent {
			live, ok := currentAgentLiveID(history, paneID)
			if !ok || pickerSelectionID(pickerKindAgent, live.String()) != selectedID {
				return pickerErrorMsg{err: fmt.Errorf("hseh focus: selected occupant was replaced")}
			}
			err = FocusHerdrAgentContext(ctx, paneID)
		} else {
			err = FocusHerdrWorkspaceContext(ctx, workspaceID)
		}
		if ctx.Err() != nil {
			return pickerErrorMsg{err: ctx.Err()}
		}
		if err != nil {
			return pickerErrorMsg{err: err}
		}
		return pickerAcceptedMsg{}
	}
}

// afterSelectionChange redirects the preview clock to the selected target. The last
// frame stays on screen; a different pane starts a selection-change read.
func (m *pickerModel) afterSelectionChange() tea.Cmd {
	item, ok := m.selectedItem()
	if !ok {
		m.stopPreviewChain()
		m.clearPreview()
		return nil
	}
	if item.Kind == pickerKindDefinition || item.PreviewPane == "" {
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

func (m *pickerModel) applyQuery() {
	m.visible, m.searchScratch = filterPickerItemsInto(m.visible[:0], m.searchScratch, m.allItems, m.query)
	if !pickerItemByID(m.visible, m.selectedID) {
		m.selectedID = ""
		if m.query == "" {
			m.selectedID = PreselectPickerItemID(m.view, m.visible, m.history, m.launch)
		}
	}
}

// refreshMembership merges fresh catalog rows without reordering rows that merely changed status.
func (m *pickerModel) refreshMembership() tea.Cmd {
	fresh := m.catalogItems()
	byID := make(map[string]PickerItem, len(fresh))
	for _, item := range fresh {
		byID[item.ID] = item
	}
	if m.query == "" {
		kept := make([]PickerItem, 0, len(fresh))
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
		var matched []PickerItem
		matched, m.searchScratch = filterPickerItemsInto(nil, m.searchScratch, fresh, m.query)
		matchedByID := make(map[string]PickerItem, len(matched))
		for _, item := range matched {
			matchedByID[item.ID] = item
		}
		visible := make([]PickerItem, 0, len(matched))
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

func (m *pickerModel) moveSelection(delta int) {
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

func (m *pickerModel) cycleView(delta int) {
	order := []string{pickerViewSpaces, pickerViewAgents}
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

func runPicker(view string) error {
	view, err := parsePickerView(view)
	if err != nil {
		return err
	}
	poll, configErrs := LoadPreviewPollInterval()
	wideMin, moreErrs := loadWidePreviewMinColumns()
	configErrs = append(configErrs, moreErrs...)
	theme, themeErrs := LoadPickerTheme("")
	configErrs = append(configErrs, themeErrs...)
	model := newAsyncPickerModel(view, theme, poll, wideMin, configErrs)
	model.mouseOut = os.Stdout
	defer model.io().cancelAll()
	_, _ = io.WriteString(os.Stdout, mouseClickOnlyEnable)
	defer func() { _, _ = io.WriteString(os.Stdout, mouseClickOnlyDisable) }()
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}
