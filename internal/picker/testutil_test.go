package picker

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
)

// newModel builds a model with snapshot, history and layout already in hand, so tests can
// drive Update without the async first-snapshot round trip.
func newModel(view string, snapshot herdr.SessionSnapshot, history focus.History, wideMin int) model {
	history = focus.Prune(history, snapshot)
	m := model{
		view:               view,
		snapshot:           snapshot,
		history:            history,
		layout:             defaultSidebarLayout(),
		widePreviewMinCols: wideMin,
		launch:             launchFromSnapshot(snapshot, history),
		snapshotReady:      true,
		catalogReady:       true,
	}
	m.rebuildVisible()
	return m
}

// setSpaceCatalog installs reusable-space definitions and associations as catalogLoadedMsg would.
func (m *model) setSpaceCatalog(definitions []space.Definition, records, unresolved []space.AssociationRecord, errs []string) {
	m.definitions = definitions
	m.associationRecords = records
	m.unresolvedRecords = unresolved
	m.catalogErrors = errs
	m.catalogReady = true
	m.recomputeStatus()
	m.rebuildVisible()
}

// flattenCmds executes a command tree (including tea.Batch) and returns the non-nil messages.
func flattenCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch batch := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, flattenCmds(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// feedCmd runs a command tree and feeds every produced message back into the model.
func feedCmd(m model, cmd tea.Cmd) model {
	for _, msg := range flattenCmds(cmd) {
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}
