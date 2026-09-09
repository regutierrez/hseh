package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitPorcelainCountsRenameConflictAndDivergence(t *testing.T) {
	payload := "# branch.oid abcdef123456\x00# branch.head topic\x00# branch.ab +2 -3\x00" +
		"2 R. N... 100644 100644 100644 abc abc R100 renamed\x00? original-name\x00" +
		"? untracked\x00u UU N... conflict\x00"
	got := parseWorkspaceGitStatus(payload)
	if got.Branch != "topic" || got.Status != "+1 ?1 !1 ↑2 ↓3" {
		t.Fatalf("%+v", got)
	}
	detached := parseWorkspaceGitStatus("# branch.oid abcdef123456\x00# branch.head (detached)\x00")
	if detached.Branch != "@abcdef1" || detached.Status != "" {
		t.Fatalf("%+v", detached)
	}
}

func TestGitUsesOnlyActivePaneDirectoryAndLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "main")
	worktree := filepath.Join(root, "linked")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, repo, "add", "tracked")
	gitFixture(t, repo, "commit", "-m", "initial")
	gitFixture(t, repo, "worktree", "add", "-b", "feature", worktree)
	snapshot := HerdrSessionSnapshot{
		Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w1", Label: "Project", ActiveTabID: "w1:t1"}},
		Layouts:    []HerdrPaneLayout{{TabID: "w1:t1", FocusedPaneID: "w1:p2"}},
		Panes:      []HerdrPaneRow{{PaneID: "w1:p1", Cwd: repo}, {PaneID: "w1:p2", Cwd: repo, ForegroundCwd: worktree}},
	}
	got := loadWorkspaceGit(context.Background(), snapshot)
	if len(got) != 1 || got[worktree].Branch != "feature" {
		t.Fatalf("not active worktree: %+v", got)
	}
	m := newPickerModel("spaces", snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout(), 0, 100)
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
	m := pickerModel{view: pickerViewSpaces}
	defer m.io().cancelAll()
	if m.startGit() == nil || !m.gitInFlight {
		t.Fatal("git refresh not started")
	}
	if m.startGit() != nil {
		t.Fatal("overlapping git refresh")
	}
	m.quitting = true
	updatePicker(&m, pickerGitLoadedMsg{byDirectory: map[string]WorkspaceGit{"old": {Branch: "stale"}}})
	if len(m.gitByDirectory) != 0 {
		t.Fatal("result applied after exit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := loadWorkspaceGit(ctx, HerdrSessionSnapshot{}); len(got) != 0 {
		t.Fatal(got)
	}
}
