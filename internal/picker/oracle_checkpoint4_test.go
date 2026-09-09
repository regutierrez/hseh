package picker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
)

func TestOracleConversationIsNotMoveProof(t *testing.T) {
	h := focus.EmptyHistory(herdr.ContinuityWitness{})
	h = focus.ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "conversation-a", false)
	h = focus.RecordAgentPaneFocus(h, "w1:p1")
	s := herdr.SessionSnapshot{Agents: []herdr.AgentRow{{PaneRow: herdr.PaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &herdr.AgentSession{Value: "conversation-a"}}}}}
	h = focus.Prune(h, s)
	for _, id := range h.Agents {
		if id.PaneID == "w2:p9" {
			t.Fatalf("conversation ref transferred to new occupant: %+v", h.Agents)
		}
	}
	items := BuildItems("agents", s, h, nil, [][]string{{"agent"}})
	got := PreselectItemID("agents", items, h, LaunchContext{})
	if got != "" && !HasItemID(items, got) {
		t.Fatalf("absent target selected: %q", got)
	}
	for _, item := range items {
		if item.ID == "w1:p1#1" {
			t.Fatal("dead pane offered in live picker")
		}
	}
}

func TestOraclePlainListRejectsTitleControls(t *testing.T) {
	s := herdr.SessionSnapshot{Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "safe\x1b[2Junsafe"}}}
	items := BuildItems("spaces", s, focus.EmptyHistory(herdr.ContinuityWitness{}), [][]string{{"workspace"}}, nil)
	b, err := EncodeListJSON(ListDocument{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	var d ListDocument
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(d.Items[0].Rows, ""), "\x1b") {
		t.Fatalf("plain JSON rows still contain terminal escapes: %q", d.Items[0].Rows)
	}
}
