package picker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
	"github.com/regutierrez/hseh/internal/space"
	"github.com/regutierrez/hseh/internal/termtext"
)

func spaceSnapshot(cwd string) herdr.SessionSnapshot {
	return herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "hseh", AgentStatus: "idle", ActiveTabID: "w1:t1"}},
		Layouts:    []herdr.PaneLayout{{TabID: "w1:t1", FocusedPaneID: "w1:p1"}},
		Panes:      []herdr.PaneRow{{PaneID: "w1:p1", Cwd: cwd}},
	}
}

func TestSpaceRowIsOneLineWithBadgeGitAndCheckoutRoot(t *testing.T) {
	cwd := "/home/u/hseh/internal/picker"
	git := map[string]gitinfo.WorkspaceGit{cwd: {Branch: "main", Status: "~2", Root: "/home/u/hseh"}}
	items := buildSpaceItems(spaceSnapshot(cwd), focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), git)
	item := items[0]
	if item.Source != SourceHerdr || item.Path != "/home/u/hseh" {
		t.Fatalf("source/path: %+v", item)
	}
	if item.PreviewPane != "" {
		t.Fatalf("space rows preview a directory, not a pane: %q", item.PreviewPane)
	}
	prefix, _ := statePrefix("idle", "dots")
	want := prefix + herdrSourceIcon + " herdr    hseh  " + gitBranchIcon + " main ~2  /home/u/hseh"
	if len(item.Rows) != 1 || item.Rows[0] != want {
		t.Fatalf("rows %q want %q", item.Rows, want)
	}
	if termtext.StripControls(item.DisplayRows[0]) != want {
		t.Fatalf("display diverges from plain: %q", item.DisplayRows[0])
	}
	// Without git the active pane's directory is the path and no git detail is shown.
	plain := buildSpaceItems(spaceSnapshot(cwd), focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), nil)[0]
	if plain.Path != cwd || !strings.HasSuffix(plain.Rows[0], "hseh  "+cwd) {
		t.Fatalf("no-git row %q path %q", plain.Rows, plain.Path)
	}
}

func TestTemplateRowIsOneLineAndKeepsDescriptionForSearchOnly(t *testing.T) {
	def := space.Definition{ID: "def-app", Name: "app", Description: "dev setup", ResolvedDir: "/srv/app", SourceFile: "/cfg/app.toml"}
	item := definitionItem(def, false, gitinfo.WorkspaceGit{Branch: "dev", Root: "/srv/app"})
	if item.Source != SourceTemplate || item.Path != "/srv/app" || len(item.Rows) != 1 {
		t.Fatalf("%+v", item)
	}
	want := "  " + templateSourceIcon + " template app  " + gitBranchIcon + " dev  /srv/app"
	if item.Rows[0] != want {
		t.Fatalf("row %q want %q", item.Rows[0], want)
	}
	if strings.Contains(item.Rows[0], "dev setup") || !strings.Contains(item.SearchText, "dev setup") || !strings.Contains(item.SearchText, "app.toml") {
		t.Fatalf("description placement: row=%q search=%q", item.Rows[0], item.SearchText)
	}
	if item.PreviewText != "" || item.Recovery != nil {
		t.Fatalf("resolved template carries recovery text: %+v", item)
	}
}

func TestBadgeColumnAlignsNamesAcrossSources(t *testing.T) {
	live := buildSpaceItems(spaceSnapshot("/x"), focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), nil)[0]
	template := definitionItem(space.Definition{ID: "d", Name: "app", ResolvedDir: "/srv/app"}, false, gitinfo.WorkspaceGit{})
	nameColumn := func(row, name string) int {
		return utf8.RuneCountInString(row[:strings.Index(row, name)])
	}
	if a, b := nameColumn(live.Rows[0], "hseh"), nameColumn(template.Rows[0], "app"); a != b {
		t.Fatalf("name columns differ: herdr=%d template=%d\n%q\n%q", a, b, live.Rows[0], template.Rows[0])
	}
}

func TestPathColumnHidesBelowMinimumListWidth(t *testing.T) {
	item := definitionItem(space.Definition{ID: "d", Name: "app", ResolvedDir: "/srv/app"}, false, gitinfo.WorkspaceGit{})
	m := model{}
	wide := termtext.StripControls(strings.Join(m.wrapItemBlock(item, false, pathColumnMinWidth), ""))
	narrow := termtext.StripControls(strings.Join(m.wrapItemBlock(item, false, pathColumnMinWidth-1), ""))
	if !strings.HasSuffix(wide, "  /srv/app") {
		t.Fatalf("wide row lost path: %q", wide)
	}
	if strings.Contains(narrow, "/srv/app") || !strings.HasSuffix(narrow, "app") {
		t.Fatalf("narrow row kept path: %q", narrow)
	}
	// A query that matches only the path still highlights without panicking when the column is hidden.
	m.query = "srv"
	filtered, _ := filterItemsInto(nil, nil, []Item{item}, "srv")
	if len(filtered) != 1 {
		t.Fatal("path no longer searchable")
	}
	if got := termtext.StripControls(strings.Join(m.wrapItemBlock(filtered[0], true, 40), "")); strings.Contains(got, "/srv") {
		t.Fatalf("narrow highlighted row shows path: %q", got)
	}
}

func TestLongSpaceRowIsTruncatedNotWrapped(t *testing.T) {
	item := definitionItem(space.Definition{ID: "d", Name: "app", ResolvedDir: "/very/long/" + strings.Repeat("segment/", 12) + "app"}, false, gitinfo.WorkspaceGit{})
	m := model{}
	lines := m.wrapItemBlock(item, true, pathColumnMinWidth)
	if len(lines) != 1 {
		t.Fatalf("row wrapped to %d lines: %q", len(lines), lines)
	}
	if ansi.StringWidth(lines[0]) != pathColumnMinWidth || !strings.HasSuffix(termtext.StripControls(lines[0]), "…") {
		t.Fatalf("row not cut to width: %q", lines[0])
	}
}

func TestListJSONCarriesSourcePathAndRecovery(t *testing.T) {
	item := definitionItem(space.Definition{ID: "def-x", Name: "x", ResolvedDir: "/srv/x"}, true, gitinfo.WorkspaceGit{})
	payload, err := EncodeListJSON(ListDocument{View: ViewSpaces, Items: []Item{item}})
	if err != nil {
		t.Fatal(err)
	}
	// encoding/json escapes "<" and ">" as \u003c and \u003e, so match the command prefix.
	for _, want := range []string{`"source": "template"`, `"path": "/srv/x"`, `"recovery": [`, `"hseh recover def-x --create"`, `"hseh recover def-x --workspace `} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("json lacks %s:\n%s", want, payload)
		}
	}
}

func directoryModel(readDir func(ctx context.Context, dir string) (string, error)) model {
	m := model{width: 120, height: 30, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true, previewLoadingDelay: time.Millisecond, selectedID: "a",
		visible: []Item{
			{Kind: KindSpace, ID: "a", Source: SourceHerdr, Path: "/tmp/a", Rows: []string{"a"}},
			{Kind: KindDefinition, ID: "b", Source: SourceTemplate, Path: "/tmp/b", PreviewText: "recovery needed\nhseh recover b --create", Rows: []string{"b"}},
		}}
	m.readDir = readDir
	return m
}

func TestSpaceSelectionListsDirectoryOnceWithoutPolling(t *testing.T) {
	var reads []string
	m := directoryModel(func(ctx context.Context, dir string) (string, error) {
		reads = append(reads, dir)
		return "listing of " + dir, nil
	})
	m = feedCmd(m, m.afterSelectionChange())
	if len(reads) != 1 || reads[0] != "/tmp/a" {
		t.Fatalf("reads %v", reads)
	}
	if m.previewText != "listing of /tmp/a" || !m.previewListing || m.previewInFlight || m.previewPane != "" {
		t.Fatalf("listing not applied: %+v", m)
	}
	if cmd := m.afterSelectionChange(); cmd != nil {
		t.Fatal("same directory re-read on reselect")
	}
	m.selectedID = "b"
	m = feedCmd(m, m.afterSelectionChange())
	if len(reads) != 2 || m.previewText != "recovery needed\nhseh recover b --create\n\nlisting of /tmp/b" {
		t.Fatalf("template preview %q reads %v", m.previewText, reads)
	}
	if !strings.Contains(termtext.StripControls(m.View()), "hseh recover b --create") {
		t.Fatal("recovery hints missing from rendered preview")
	}
}

func TestDirectoryPreviewReplyStartsNoPollTick(t *testing.T) {
	m := directoryModel(nil)
	m.previewInFlight, m.previewSeq, m.previewDir = true, 3, "/tmp/a"
	next, cmd := m.Update(previewLoadedMsg{seq: 3, targetID: "a", dir: "/tmp/a", text: "listing"})
	if cmd != nil {
		t.Fatal("directory preview scheduled a poll tick")
	}
	if got := next.(model); got.previewText != "listing" || !got.previewListing {
		t.Fatalf("%+v", got)
	}
	// A reply for another directory (stale target) is ignored.
	next, _ = next.(model).Update(previewLoadedMsg{seq: 3, targetID: "a", dir: "/tmp/other", text: "other"})
	if got := next.(model); got.previewText != "listing" {
		t.Fatalf("stale directory applied: %q", got.previewText)
	}
}

func TestDirectoryPreviewErrorIsIsolated(t *testing.T) {
	m := directoryModel(func(context.Context, string) (string, error) { return "", errors.New("eza: no such directory") })
	m = feedCmd(m, m.afterSelectionChange())
	if m.previewErr != "eza: no such directory" || m.previewText != "" {
		t.Fatalf("err=%q text=%q", m.previewErr, m.previewText)
	}
	view := termtext.StripControls(m.View())
	if !strings.Contains(view, copyPreviewFailed) || !strings.Contains(view, "no such directory") {
		t.Fatalf("view %q", view)
	}
	if cmd := m.startAccept(); cmd == nil {
		t.Fatal("Enter blocked by a failed preview")
	}
	m.io().cancelAll()
}

func TestListingPreviewTruncatesInsteadOfWrapping(t *testing.T) {
	m := directoryModel(nil)
	m.previewDir, m.previewListing = "/tmp/a", true
	m.previewText = "\x1b[34mfirst\x1b[0m\n" + strings.Repeat("x", 300) + "\nlast"
	f := m.frame()
	rows := strings.Split(m.renderPreview(f.previewW, f.previewH), "\n")
	if len(rows) != f.previewH {
		t.Fatalf("rows %d want %d", len(rows), f.previewH)
	}
	xRows := 0
	for _, row := range rows {
		if ansi.StringWidth(row) != f.previewW {
			t.Fatalf("row width %d want %d: %q", ansi.StringWidth(row), f.previewW, row)
		}
		if strings.Contains(row, "xxxx") {
			xRows++
		}
	}
	if xRows != 1 || !strings.Contains(termtext.StripControls(rows[0]), "first") || !strings.Contains(termtext.StripControls(rows[2]), "last") {
		t.Fatalf("listing wrapped or reordered: %q", rows[:3])
	}
}

func TestGitCoversTemplateDirectories(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "tmpl")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	hsehtest.Git(t, repo, "init", "-b", "main")
	def := space.Definition{ID: "def-t", Name: "tmpl", ResolvedDir: repo, WorkingDir: repo}
	got := LoadGit(context.Background(), herdr.SessionSnapshot{}, []space.Definition{def})
	if got[repo].Branch != "main" || got[repo].Root != repo {
		t.Fatalf("template git %+v", got)
	}
	m := newModel("spaces", herdr.SessionSnapshot{Version: "0.9.0"}, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), 0, 100)
	m.setSpaceCatalog([]space.Definition{def}, nil, nil, nil)
	m.gitByDirectory = got
	if row := m.catalogItems()[0].Rows[0]; !strings.Contains(row, gitBranchIcon+" main") {
		t.Fatalf("template row lacks git: %q", row)
	}
}
