package main

import tea "github.com/charmbracelet/bubbletea"

type pickerGitLoadedMsg struct{ byDirectory map[string]WorkspaceGit }

// Git refresh is independent of live status/preview reads. A slow checkout cannot freeze those updates.
func (m *pickerModel) startGit() tea.Cmd {
	if m.quitting || m.gitInFlight || m.view == pickerViewAgents {
		return nil
	}
	m.gitInFlight = true
	snapshot := m.snapshot
	ctx := m.io().replace(&m.io().git)
	return func() tea.Msg { return pickerGitLoadedMsg{byDirectory: loadWorkspaceGit(ctx, snapshot)} }
}
