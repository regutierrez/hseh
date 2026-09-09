package picker

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/trace"
)

type gitLoadedMsg struct {
	byDirectory map[string]gitinfo.WorkspaceGit
}

// Git refresh is independent of live status/preview reads. A slow checkout cannot freeze those updates.
func (m *model) startGit() tea.Cmd {
	if m.quitting || m.gitInFlight || m.view == ViewAgents {
		return nil
	}
	m.gitInFlight = true
	snapshot := m.snapshot
	ctx := m.io().replace(&m.io().git)
	return func() tea.Msg { return gitLoadedMsg{byDirectory: LoadWorkspaceGit(ctx, snapshot)} }
}

func LoadWorkspaceGit(ctx context.Context, snapshot herdr.SessionSnapshot) map[string]gitinfo.WorkspaceGit {
	byDirectory := map[string]gitinfo.WorkspaceGit{}
	span := trace.Span("git.load")
	for _, workspace := range snapshot.Workspaces {
		if ctx.Err() != nil {
			break
		}
		dir := herdr.ActiveWorkspaceDirectory(snapshot, workspace.WorkspaceID)
		if dir == "" {
			continue
		}
		if _, exists := byDirectory[dir]; !exists {
			byDirectory[dir] = gitinfo.Read(ctx, dir)
		}
	}
	span("dirs", len(byDirectory))
	return byDirectory
}
