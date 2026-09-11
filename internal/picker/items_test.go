package picker

import (
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
)

func TestRenderSidebarRowsOmitsMissingValues(t *testing.T) {
	rows, _ := renderSidebarRows(tokensFromNames([][]string{{"state_icon", "workspace", "tab"}, {"agent"}, {"$name2"}}), map[string]string{
		"state_icon": "idle",
		"workspace":  "hseh",
		"agent":      "pi",
	}, "idle")
	if len(rows) != 2 || rows[0] != "idle · hseh" || rows[1] != "pi" {
		t.Fatalf("got %#v", rows)
	}
}

func TestSpaceRowsStripTitleControls(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "safe\x1b[2Junsafe"}}}
	items := buildSpaceItems(snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), nil)
	if strings.Contains(strings.Join(items[0].Rows, ""), "\x1b") || strings.Contains(items[0].Label, "\x1b") {
		t.Fatalf("terminal controls survive in plain rows: %q label %q", items[0].Rows, items[0].Label)
	}
}

func TestPreselectPickerItemIDPrefersPreviousNotCurrent(t *testing.T) {
	items := []Item{
		{Kind: KindSpace, ID: selectionID(KindSpace, "w1"), WorkspaceID: "w1"},
		{Kind: KindSpace, ID: selectionID(KindSpace, "w2"), WorkspaceID: "w2"},
	}
	history := focus.History{Spaces: []string{"w1", "w2"}}
	launch := launchContext{CurrentSpace: "w1"}
	got := preselectItemID(ViewSpaces, items, history, launch)
	if got != selectionID(KindSpace, "w2") {
		t.Fatalf("got %q", got)
	}
}

func TestPreselectPickerItemIDAgentBelowPriority(t *testing.T) {
	items := []Item{
		{Kind: KindAgent, ID: selectionID(KindAgent, "w1:p1#1"), PaneID: "w1:p1"},
		{Kind: KindAgent, ID: selectionID(KindAgent, "w1:p2#1"), PaneID: "w1:p2"},
	}
	history := focus.History{Agents: []focus.AgentLiveID{{PaneID: "w1:p2", Generation: 1}}}
	got := preselectItemID(ViewAgents, items, history, launchContext{CurrentAgent: "w1:p9#1"})
	if got != selectionID(KindAgent, "w1:p2#1") {
		t.Fatalf("got %q", got)
	}
}

func TestFilterPickerItemsKeepsViewOrderOnScoreTie(t *testing.T) {
	items := []Item{
		{ID: "a", SearchText: "alpha workspace"},
		{ID: "b", SearchText: "alpha workspace"},
	}
	got, _ := filterItemsInto(nil, nil, items, "alpha")
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("got %#v", got)
	}
}

func TestAgentRowsProjectUnpresentedIdleAsDone(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Agents: []herdr.AgentRow{
		{PaneRow: herdr.PaneRow{PaneID: "w1:p1", Agent: "pi", AgentStatus: "working"}, StateChangeSeq: 4},
	}}
	history := focus.Prune(focus.EmptyHistory(herdr.ContinuityWitness{}), snapshot)
	snapshot.Agents[0].AgentStatus = "idle"
	snapshot.Agents[0].StateChangeSeq = 5
	history = focus.Prune(history, snapshot)
	items := buildAgentItems(snapshot, history, defaultSidebarLayout())
	if len(items) != 1 || items[0].Status != "done" {
		t.Fatalf("unpresented idle must render as done, got %+v", items)
	}
}

func TestPreselectSkipsAbsentHistoryTarget(t *testing.T) {
	items := []Item{{Kind: KindAgent, ID: selectionID(KindAgent, "w1:p2#1"), PaneID: "w1:p2"}}
	history := focus.History{Agents: []focus.AgentLiveID{{PaneID: "w1:p1", Generation: 1}, {PaneID: "w1:p2", Generation: 1}}}
	got := preselectItemID(ViewAgents, items, history, launchContext{CurrentAgent: "w9:p9#1"})
	if got != selectionID(KindAgent, "w1:p2#1") {
		t.Fatalf("absent history target offered: %q", got)
	}
}
