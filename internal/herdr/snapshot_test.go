package herdr_test

import (
	"context"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestFocusHerdrAgentContextSyncsReturnedTab(t *testing.T) {
	server := &hsehtest.Server{}
	server.Snapshot.Agents = []herdr.AgentRow{{PaneRow: herdr.PaneRow{
		PaneID:      "w1:p2",
		TabID:       "w1:t2",
		WorkspaceID: "w1",
		Agent:       "pi",
	}}}
	socketPath, _ := hsehtest.Start(t, server)
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	if err := herdr.FocusAgentContext(context.Background(), "w1:p2"); err != nil {
		t.Fatal(err)
	}
	methods := server.Methods()
	if len(methods) != 2 || methods[0] != "agent.focus" || methods[1] != "tab.focus" {
		t.Fatalf("methods %v", methods)
	}
	if focusedTab, _ := server.FocusedTab.Load().(string); focusedTab != "w1:t2" {
		t.Fatalf("tab.focus used %q, want returned agent tab_id", focusedTab)
	}
}

func TestFocusHerdrAgentContextRejectsMissingTabID(t *testing.T) {
	server := &hsehtest.Server{}
	socketPath, _ := hsehtest.Start(t, server)
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	err := herdr.FocusAgentContext(context.Background(), "w1:p2")
	if err == nil || !strings.Contains(err.Error(), "hseh focus: agent.focus missing tab_id") {
		t.Fatalf("got %v", err)
	}
	if methods := server.Methods(); len(methods) != 1 || methods[0] != "agent.focus" {
		t.Fatalf("guessed fallback after malformed agent.focus: %v", methods)
	}
}
