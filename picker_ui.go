package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type previewLoadedMsg struct {
	seq      uint64
	targetID string
	paneID   string
	text     string
	err      error
}

type snapshotTickMsg struct{}

type snapshotLoadedMsg struct {
	seq        uint64
	snapshot   HerdrSessionSnapshot
	history    FocusHistory
	records    []SpaceAssociationRecord
	unresolved []SpaceAssociationRecord
	liveErr    error
	catalogErr error
}

type pickerAcceptedMsg struct{}

type pickerErrorMsg struct{ err error }

type pickerCancels struct {
	mu       sync.Mutex
	preview  context.CancelFunc
	snapshot context.CancelFunc
	accept   context.CancelFunc
	git      context.CancelFunc
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
	if c.preview != nil {
		c.preview()
		c.preview = nil
	}
	if c.snapshot != nil {
		c.snapshot()
		c.snapshot = nil
	}
	if c.accept != nil {
		c.accept()
		c.accept = nil
	}
	if c.git != nil {
		c.git()
		c.git = nil
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
	previewText        string
	previewErr         string
	statusErr          string
	pollEvery          time.Duration
	widePreviewMinCols int
	launch             pickerLaunchContext
	snapshotSeq        uint64
	previewSeq         uint64
	snapshotInFlight   bool
	gitInFlight        bool
	gitByDirectory     map[string]WorkspaceGit
	previewInFlight    bool
	pendingAccept      bool
	quitting           bool
	cancels            *pickerCancels
	previewPane        string
}

func newPickerModel(view string, snapshot HerdrSessionSnapshot, history FocusHistory, layout SidebarLayout, pollEvery time.Duration, wideMin int) pickerModel {
	history = pruneFocusHistory(history, snapshot)
	launch := pickerLaunchFromEnv(snapshot, history)
	model := pickerModel{
		view:               view,
		snapshot:           snapshot,
		history:            history,
		layout:             layout,
		pollEvery:          pollEvery,
		widePreviewMinCols: wideMin,
		launch:             launch,
		cancels:            &pickerCancels{},
	}
	model.rebuildVisible()
	return model
}

func (m *pickerModel) setSpaceCatalog(definitions []SpaceDefinition, records, unresolved []SpaceAssociationRecord, errs []string) {
	m.definitions = definitions
	m.associationRecords = records
	m.unresolvedRecords = unresolved
	m.catalogErrors = errs
	if m.statusErr == "" && len(errs) > 0 {
		m.statusErr = strings.Join(errs, "; ")
	}
	m.rebuildVisible()
}

func (m *pickerModel) rebuildVisible() {
	items := m.catalogItems()
	m.allItems = items
	m.visible = FilterPickerItems(items, m.query)
	m.selectedID = PreselectPickerItemID(m.view, m.visible, m.history, m.launch)
	if item, ok := m.selectedItem(); ok {
		m.previewPane = item.PreviewPane
		if item.Kind == pickerKindDefinition {
			m.previewText = item.PreviewText
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
	return func() tea.Msg { return snapshotTickMsg{} }
}

func tickSnapshot(every time.Duration) tea.Cmd {
	if every <= 0 {
		every = time.Duration(DefaultPreviewPollMilliseconds) * time.Millisecond
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return snapshotTickMsg{} })
}

func (m pickerModel) wideEnoughForPreview() bool {
	return m.width >= 3 && m.width >= m.widePreviewMinCols && m.widePreviewMinCols > 0
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
	cmd := updatePicker(&m, msg)
	return m, cmd
}

func updatePicker(m *pickerModel, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		wasWide := m.wideEnoughForPreview()
		m.width = msg.Width
		m.height = msg.Height
		nowWide := m.wideEnoughForPreview()
		if !nowWide {
			m.clearPreview()
			m.io().cancelPreview()
			return nil
		}
		if nowWide && !wasWide {
			return m.startPreview()
		}
		return nil
	case snapshotTickMsg:
		if m.quitting {
			return nil
		}
		return tea.Batch(tickSnapshot(m.pollEvery), m.startSnapshot(), m.startPreview(), m.startGit())
	case pickerGitLoadedMsg:
		m.gitInFlight = false
		if m.quitting {
			return nil
		}
		m.gitByDirectory = msg.byDirectory
		m.refreshMembership()
		return nil
	case snapshotLoadedMsg:
		if m.quitting || msg.seq != m.snapshotSeq {
			return nil
		}
		m.snapshotInFlight = false
		if msg.liveErr != nil {
			m.statusErr = msg.liveErr.Error()
			return nil
		}
		m.snapshot = msg.snapshot
		m.history = msg.history
		if msg.catalogErr != nil {
			m.associationRecords = nil
			m.unresolvedRecords = nil
			m.statusErr = msg.catalogErr.Error()
		} else {
			m.associationRecords = msg.records
			m.unresolvedRecords = msg.unresolved
			if len(m.catalogErrors) > 0 {
				m.statusErr = strings.Join(m.catalogErrors, "; ")
			} else {
				m.statusErr = ""
			}
		}
		m.refreshMembership()
		return nil
	case previewLoadedMsg:
		if msg.seq != m.previewSeq {
			return nil
		}
		m.previewInFlight = false
		if m.quitting {
			return nil
		}
		item, ok := m.selectedItem()
		if !ok || msg.targetID != m.selectedID || msg.paneID != item.PreviewPane {
			m.clearPreview()
			return nil
		}
		if msg.err != nil {
			m.statusErr = msg.err.Error()
			m.previewErr = msg.err.Error()
			m.previewText = ""
			return nil
		}
		if !m.wideEnoughForPreview() {
			m.clearPreview()
			return nil
		}
		m.previewErr = ""
		m.previewText = msg.text
		return nil
	case pickerAcceptedMsg:
		m.quitting = true
		m.io().cancelAll()
		return tea.Quit
	case pickerErrorMsg:
		m.pendingAccept = false
		m.statusErr = msg.err.Error()
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
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			return nil
		}
		id := m.itemIDAtMouse(msg.X, msg.Y)
		if id == "" || id == m.selectedID {
			return nil
		}
		m.selectedID = id
		return m.afterSelectionChange()
	}
	return nil
}

func (m *pickerModel) startSnapshot() tea.Cmd {
	if m.quitting || m.snapshotInFlight {
		return nil
	}
	m.snapshotInFlight = true
	m.snapshotSeq++
	seq := m.snapshotSeq
	ctx := m.io().replace(&m.io().snapshot)
	return func() tea.Msg {
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

func (m *pickerModel) startPreview() tea.Cmd {
	if m.quitting || m.previewInFlight || !m.wideEnoughForPreview() {
		return nil
	}
	item, ok := m.selectedItem()
	if !ok {
		m.clearPreview()
		return nil
	}
	if item.Kind == pickerKindDefinition {
		m.previewPane = ""
		m.previewErr = ""
		m.previewText = item.PreviewText
		return nil
	}
	if item.PreviewPane == "" {
		m.clearPreview()
		return nil
	}
	m.previewInFlight = true
	m.previewSeq++
	seq := m.previewSeq
	targetID := item.ID
	paneID := item.PreviewPane
	m.previewPane = paneID
	ctx := m.io().replace(&m.io().preview)
	return func() tea.Msg {
		result, _, err := ReadHerdrPaneVisibleANSIContext(ctx, paneID)
		if err != nil {
			return previewLoadedMsg{seq: seq, targetID: targetID, paneID: paneID, err: err}
		}
		return previewLoadedMsg{seq: seq, targetID: targetID, paneID: paneID, text: AllowVisiblePreviewANSI(result.Text)}
	}
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

func (m *pickerModel) clearPreview() {
	m.previewText = ""
	m.previewErr = ""
	m.previewPane = ""
}

func (m *pickerModel) afterSelectionChange() tea.Cmd {
	item, ok := m.selectedItem()
	if !ok || item.PreviewPane != m.previewPane {
		m.clearPreview()
		m.io().cancelPreview()
		m.previewInFlight = false
	}
	if !ok {
		return nil
	}
	return m.startPreview()
}

func (m *pickerModel) applyQuery() {
	fresh := m.catalogItems()
	m.allItems = fresh
	if m.query == "" {
		m.visible = fresh
	} else {
		m.visible = FilterPickerItems(fresh, m.query)
	}
	if !pickerItemByID(m.visible, m.selectedID) {
		m.selectedID = ""
		if m.query == "" {
			m.selectedID = PreselectPickerItemID(m.view, m.visible, m.history, m.launch)
		}
		m.clearPreview()
	}
}

func (m *pickerModel) refreshMembership() {
	fresh := m.catalogItems()
	byID := map[string]PickerItem{}
	for _, item := range fresh {
		byID[item.ID] = item
	}
	if m.query == "" {
		var kept []PickerItem
		seen := map[string]bool{}
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
		m.visible = kept
	} else {
		matched := FilterPickerItems(fresh, m.query)
		matchedIDs := map[string]bool{}
		for _, item := range matched {
			matchedIDs[item.ID] = true
		}
		var visible []PickerItem
		seen := map[string]bool{}
		for _, item := range m.visible {
			if next, ok := byID[item.ID]; ok && matchedIDs[item.ID] {
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
		m.clearPreview()
		return
	}
	if item.PreviewPane != m.previewPane {
		m.clearPreview()
	}
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
	m.applyQuery()
}

func runPicker(view string) error {
	view, err := parsePickerView(view)
	if err != nil {
		return err
	}
	snapshot, witness, err := LoadHerdrSessionSnapshot()
	if err != nil {
		return err
	}
	history, err := LoadPrunedFocusHistory(snapshot, witness)
	if err != nil {
		return err
	}
	layout, configErrs := LoadSidebarLayout("")
	poll, moreErrs := LoadPreviewPollInterval()
	configErrs = append(configErrs, moreErrs...)
	wideMin, moreErrs := loadWidePreviewMinColumns()
	configErrs = append(configErrs, moreErrs...)
	definitions, defErrs := LoadSpaceDefinitions(spaceDefinitionsDir())
	configErrs = append(configErrs, defErrs...)
	state, assocErr := loadReconciledSpaceAssociationState(pluginStateDir(), witness)
	var records, unresolved []SpaceAssociationRecord
	if assocErr != nil {
		configErrs = append(configErrs, assocErr.Error())
	} else {
		records = state.Records
		unresolved = state.Unresolved
	}
	model := newPickerModel(view, snapshot, history, layout, poll, wideMin)
	model.setSpaceCatalog(definitions, records, unresolved, configErrs)
	defer model.io().cancelAll()
	_, err = tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
