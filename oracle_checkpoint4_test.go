package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOracleConversationIsNotMoveProof(t *testing.T) {
	h := emptyFocusHistory(ServerContinuityWitness{})
	h = ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "conversation-a", false)
	h = RecordAgentPaneFocus(h, "w1:p1")
	s := HerdrSessionSnapshot{Agents: []HerdrAgentRow{{HerdrPaneRow: HerdrPaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "conversation-a"}}}}}
	h = pruneFocusHistory(h, s)
	for _, id := range h.Agents {
		if id.PaneID == "w2:p9" {
			t.Fatalf("conversation ref transferred to new occupant: %+v", h.Agents)
		}
	}
	items := BuildPickerItems("agents", s, h, nil, [][]string{{"agent"}})
	got := PreselectPickerItemID("agents", items, h, pickerLaunchContext{})
	if got != "" && !pickerItemByID(items, got) {
		t.Fatalf("absent target selected: %q", got)
	}
	for _, item := range items {
		if item.ID == "w1:p1#1" {
			t.Fatal("dead pane offered in live picker")
		}
	}
}

func TestOraclePlainListRejectsTitleControls(t *testing.T) {
	s := HerdrSessionSnapshot{Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w1", Label: "safe\x1b[2Junsafe"}}}
	items := BuildPickerItems("spaces", s, emptyFocusHistory(ServerContinuityWitness{}), [][]string{{"workspace"}}, nil)
	b, err := encodePickerListJSON(PickerListDocument{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	var d PickerListDocument
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(d.Items[0].Rows, ""), "\x1b") {
		t.Fatalf("plain JSON rows still contain terminal escapes: %q", d.Items[0].Rows)
	}
}
