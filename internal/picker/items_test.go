package picker

import (
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/termtext"
)

func TestRenderTokenRowsOmitsMissingValues(t *testing.T) {
	rows := renderTokenRows([][]string{{"state_icon", "workspace", "tab"}, {"agent"}, {"$name2"}}, map[string]string{
		"state_icon": "idle",
		"workspace":  "hseh",
		"agent":      "pi",
	})
	if len(rows) != 2 || termtext.StripControls(rows[0]) != "idle · hseh" || termtext.StripControls(rows[1]) != "pi" {
		t.Fatalf("got %#v", rows)
	}
}

func TestPreselectPickerItemIDPrefersPreviousNotCurrent(t *testing.T) {
	items := []Item{
		{Kind: KindSpace, ID: SelectionID(KindSpace, "w1"), WorkspaceID: "w1"},
		{Kind: KindSpace, ID: SelectionID(KindSpace, "w2"), WorkspaceID: "w2"},
	}
	history := focus.History{Spaces: []string{"w1", "w2"}}
	launch := LaunchContext{CurrentSpace: "w1"}
	got := PreselectItemID(ViewSpaces, items, history, launch)
	if got != SelectionID(KindSpace, "w2") {
		t.Fatalf("got %q", got)
	}
}

func TestPreselectPickerItemIDAgentBelowPriority(t *testing.T) {
	items := []Item{
		{Kind: KindAgent, ID: SelectionID(KindAgent, "w1:p1#1"), PaneID: "w1:p1"},
		{Kind: KindAgent, ID: SelectionID(KindAgent, "w1:p2#1"), PaneID: "w1:p2"},
	}
	history := focus.History{Agents: []focus.AgentLiveID{{PaneID: "w1:p2", Generation: 1}}}
	got := PreselectItemID(ViewAgents, items, history, LaunchContext{CurrentAgent: "w1:p9#1"})
	if got != SelectionID(KindAgent, "w1:p2#1") {
		t.Fatalf("got %q", got)
	}
}

func TestFilterPickerItemsKeepsViewOrderOnScoreTie(t *testing.T) {
	items := []Item{
		{ID: "a", SearchText: "alpha workspace"},
		{ID: "b", SearchText: "alpha workspace"},
	}
	got := FilterItems(items, "alpha")
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("got %#v", got)
	}
}

func TestPreselectSkipsAbsentHistoryTarget(t *testing.T) {
	items := []Item{{Kind: KindAgent, ID: SelectionID(KindAgent, "w1:p2#1"), PaneID: "w1:p2"}}
	history := focus.History{Agents: []focus.AgentLiveID{{PaneID: "w1:p1", Generation: 1}, {PaneID: "w1:p2", Generation: 1}}}
	got := PreselectItemID(ViewAgents, items, history, LaunchContext{CurrentAgent: "w9:p9#1"})
	if got != SelectionID(KindAgent, "w1:p2#1") {
		t.Fatalf("absent history target offered: %q", got)
	}
}

func TestBuildPickerItemsAgentsUsePriority(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w1:pIdle", AgentStatus: "idle", DisplayAgent: "idle"}, StateChangeSeq: 9},
			{PaneRow: herdr.PaneRow{PaneID: "w1:pBlock", AgentStatus: "blocked", DisplayAgent: "block"}, StateChangeSeq: 1},
		},
	}
	items := buildAgentItems(snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout())
	if items[0].PaneID != "w1:pBlock" {
		t.Fatalf("blocked should rank first, got %#v", items[0])
	}
}
