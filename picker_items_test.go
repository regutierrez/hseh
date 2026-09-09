package main

import "testing"

func TestRenderTokenRowsOmitsMissingValues(t *testing.T) {
	rows := renderTokenRows([][]string{{"state_icon", "workspace", "tab"}, {"agent"}, {"$name2"}}, map[string]string{
		"state_icon": "idle",
		"workspace":  "hseh",
		"agent":      "pi",
	})
	if len(rows) != 2 || StripTerminalControls(rows[0]) != "idle · hseh" || StripTerminalControls(rows[1]) != "pi" {
		t.Fatalf("got %#v", rows)
	}
}

func TestPreselectPickerItemIDPrefersPreviousNotCurrent(t *testing.T) {
	items := []PickerItem{
		{Kind: pickerKindSpace, ID: pickerSelectionID(pickerKindSpace, "w1"), WorkspaceID: "w1"},
		{Kind: pickerKindSpace, ID: pickerSelectionID(pickerKindSpace, "w2"), WorkspaceID: "w2"},
	}
	history := FocusHistory{Spaces: []string{"w1", "w2"}}
	launch := pickerLaunchContext{CurrentSpace: "w1"}
	got := PreselectPickerItemID(pickerViewSpaces, items, history, launch)
	if got != pickerSelectionID(pickerKindSpace, "w2") {
		t.Fatalf("got %q", got)
	}
}

func TestPreselectPickerItemIDAgentBelowPriority(t *testing.T) {
	items := []PickerItem{
		{Kind: pickerKindAgent, ID: pickerSelectionID(pickerKindAgent, "w1:p1#1"), PaneID: "w1:p1"},
		{Kind: pickerKindAgent, ID: pickerSelectionID(pickerKindAgent, "w1:p2#1"), PaneID: "w1:p2"},
	}
	history := FocusHistory{Agents: []AgentLiveID{{PaneID: "w1:p2", Generation: 1}}}
	got := PreselectPickerItemID(pickerViewAgents, items, history, pickerLaunchContext{CurrentAgent: "w1:p9#1"})
	if got != pickerSelectionID(pickerKindAgent, "w1:p2#1") {
		t.Fatalf("got %q", got)
	}
}

func TestFilterPickerItemsKeepsViewOrderOnScoreTie(t *testing.T) {
	items := []PickerItem{
		{ID: "a", SearchText: "alpha workspace"},
		{ID: "b", SearchText: "alpha workspace"},
	}
	got := FilterPickerItems(items, "alpha")
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("got %#v", got)
	}
}

func TestPreselectSkipsAbsentHistoryTarget(t *testing.T) {
	items := []PickerItem{{Kind: pickerKindAgent, ID: pickerSelectionID(pickerKindAgent, "w1:p2#1"), PaneID: "w1:p2"}}
	history := FocusHistory{Agents: []AgentLiveID{{PaneID: "w1:p1", Generation: 1}, {PaneID: "w1:p2", Generation: 1}}}
	got := PreselectPickerItemID(pickerViewAgents, items, history, pickerLaunchContext{CurrentAgent: "w9:p9#1"})
	if got != pickerSelectionID(pickerKindAgent, "w1:p2#1") {
		t.Fatalf("absent history target offered: %q", got)
	}
}

func TestBuildPickerItemsAgentsUsePriority(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Agents: []HerdrAgentRow{
			{HerdrPaneRow: HerdrPaneRow{PaneID: "w1:pIdle", AgentStatus: "idle", DisplayAgent: "idle"}, StateChangeSeq: 9},
			{HerdrPaneRow: HerdrPaneRow{PaneID: "w1:pBlock", AgentStatus: "blocked", DisplayAgent: "block"}, StateChangeSeq: 1},
		},
	}
	items := buildAgentPickerItems(snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout())
	if items[0].PaneID != "w1:pBlock" {
		t.Fatalf("blocked should rank first, got %#v", items[0])
	}
}
