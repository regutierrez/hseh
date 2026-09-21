package picker

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
)

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

func (m *model) setSpaceCatalog(definitions []space.Definition, records, unresolved []space.AssociationRecord, errs []string) {
	m.definitions = definitions
	m.associationRecords = records
	m.unresolvedRecords = unresolved
	m.catalogErrors = errs
	m.catalogReady = true
	m.recomputeStatus()
	m.rebuildVisible()
}

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

func feedCmd(m model, cmd tea.Cmd) model {
	for _, msg := range flattenCmds(cmd) {
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}
