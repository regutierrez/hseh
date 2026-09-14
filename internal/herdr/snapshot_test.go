package herdr_test

import (
	"context"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestFocusHerdrAgentContextFocusesTabOnly(t *testing.T) {
	server := &hsehtest.Server{}
	socketPath, _ := hsehtest.Start(t, server)
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	if err := herdr.FocusAgentContext(context.Background(), "w1:t2"); err != nil {
		t.Fatal(err)
	}
	methods := server.Methods()
	if len(methods) != 1 || methods[0] != "tab.focus" {
		t.Fatalf("methods %v", methods)
	}
	if focusedTab, _ := server.FocusedTab.Load().(string); focusedTab != "w1:t2" {
		t.Fatalf("tab.focus used %q, want agent tab_id", focusedTab)
	}
}

func TestFocusHerdrAgentContextRejectsMissingTabID(t *testing.T) {
	server := &hsehtest.Server{}
	socketPath, _ := hsehtest.Start(t, server)
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	err := herdr.FocusAgentContext(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "hseh focus: missing tab_id") {
		t.Fatalf("got %v", err)
	}
	if methods := server.Methods(); len(methods) != 0 {
		t.Fatalf("guessed fallback with empty tab_id: %v", methods)
	}
}
