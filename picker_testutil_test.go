package main

import tea "github.com/charmbracelet/bubbletea"

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
func feedCmd(m pickerModel, cmd tea.Cmd) pickerModel {
	for _, msg := range flattenCmds(cmd) {
		next, _ := m.Update(msg)
		m = next.(pickerModel)
	}
	return m
}
