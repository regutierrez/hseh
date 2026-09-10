package picker

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
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
	definitions := m.definitions
	ctx := m.io().replace(&m.io().git)
	return func() tea.Msg { return gitLoadedMsg{byDirectory: loadGit(ctx, snapshot, definitions)} }
}

// loadGit reads git detail for every live workspace's active directory and every template directory.
func loadGit(ctx context.Context, snapshot herdr.SessionSnapshot, definitions []space.Definition) map[string]gitinfo.WorkspaceGit {
	byDirectory := map[string]gitinfo.WorkspaceGit{}
	span := trace.Span("git.load")
	dirs := make([]string, 0, len(snapshot.Workspaces)+len(definitions))
	for _, workspace := range snapshot.Workspaces {
		dirs = append(dirs, herdr.ActiveWorkspaceDirectory(snapshot, workspace.WorkspaceID))
	}
	for _, def := range definitions {
		dirs = append(dirs, def.ResolvedDir)
	}
	for _, dir := range dirs {
		if ctx.Err() != nil {
			break
		}
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
