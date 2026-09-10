package picker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestGitUsesOnlyActivePaneDirectoryAndLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "main")
	worktree := filepath.Join(root, "linked")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	hsehtest.Git(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	hsehtest.Git(t, repo, "add", "tracked")
	hsehtest.Git(t, repo, "commit", "-m", "initial")
	hsehtest.Git(t, repo, "worktree", "add", "-b", "feature", worktree)
	snapshot := herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "Project", ActiveTabID: "w1:t1"}},
		Layouts:    []herdr.PaneLayout{{TabID: "w1:t1", FocusedPaneID: "w1:p2"}},
		Panes:      []herdr.PaneRow{{PaneID: "w1:p1", Cwd: repo}, {PaneID: "w1:p2", Cwd: repo, ForegroundCwd: worktree}},
	}
	got := loadGit(context.Background(), snapshot, nil)
	if len(got) != 1 || got[worktree].Branch != "feature" {
		t.Fatalf("not active worktree: %+v", got)
	}
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), 100)
	m.gitByDirectory = got
	if !strings.Contains(m.catalogItems()[0].SearchText, "feature") {
		t.Fatal("worktree branch missing")
	}
	m.snapshot.Panes[1].ForegroundCwd = repo
	if strings.Contains(m.catalogItems()[0].SearchText, "feature") {
		t.Fatal("old directory git result rendered after pane directory change")
	}
}

func TestGitRefreshDoesNotOverlapAndCancelsOnExit(t *testing.T) {
	m := model{view: ViewSpaces}
	defer m.io().cancelAll()
	if m.startGit() == nil || !m.gitInFlight {
		t.Fatal("git refresh not started")
	}
	if m.startGit() != nil {
		t.Fatal("overlapping git refresh")
	}
	m.quitting = true
	update(&m, gitLoadedMsg{byDirectory: map[string]gitinfo.WorkspaceGit{"old": {Branch: "stale"}}})
	if len(m.gitByDirectory) != 0 {
		t.Fatal("result applied after exit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := loadGit(ctx, herdr.SessionSnapshot{}, nil); len(got) != 0 {
		t.Fatal(got)
	}
}
