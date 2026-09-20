package picker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
)

func TestLoadSidebarLayoutUsesHerdrSymbolIndicators(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nstatus_indicators = \"symbols\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	layout, errs := loadSidebarLayout(path)
	if len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
	if layout.StatusIndicators != "symbols" {
		t.Fatalf("status indicators %q", layout.StatusIndicators)
	}
	plain, _ := renderSpaceRow(spaceRow{status: "working", source: SourceHerdr, name: "alpha"}, layout.StatusIndicators, colorTheme{})
	if !strings.HasPrefix(plain, "◐ ") {
		t.Fatalf("space working %q", plain)
	}
	snapshot := herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "alpha"}},
		Tabs:       []herdr.TabRow{{TabID: "w1:t1", Label: "tab"}},
	}
	agent := herdr.AgentRow{PaneRow: herdr.PaneRow{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "pi", AgentStatus: "done"}}
	rows, _ := renderAgentSidebarRows(snapshot, agent, layout, colorTheme{})
	if len(rows) == 0 || !strings.HasPrefix(rows[0], "✓ ") {
		t.Fatalf("agent done %q", rows)
	}
}

func TestLoadSidebarLayoutKeepsOrdinaryRowStyles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[ui.sidebar.agents]
rows = [["state_icon", "workspace", "tab"], [{ token = "workspace", fg = "#89b4fa", bold = true }]]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	layout, errs := loadSidebarLayout(path)
	if len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
	if len(layout.AgentRows) != 2 || !layout.AgentRows[1][0].Styled || layout.AgentRows[1][0].Fg != "#89b4fa" {
		t.Fatalf("style dropped: %+v", layout.AgentRows)
	}
	items := buildItemsWithLayout("agents", herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "alpha"}},
		Agents:     []herdr.AgentRow{{PaneRow: herdr.PaneRow{PaneID: "w1:p1", WorkspaceID: "w1", Agent: "pi", AgentStatus: "idle"}}},
	}, focus.EmptyHistory(herdr.ContinuityWitness{}), layout, nil, colorTheme{})
	if strings.Contains(strings.Join(items[0].Rows, ""), "\x1b") {
		t.Fatalf("JSON rows styled: %q", items[0].Rows)
	}
	if len(items[0].Rows) != 3 || items[0].Rows[2] != "alpha" {
		t.Fatalf("plain rows %q", items[0].Rows)
	}
}

func TestSidebarRulesColorAndHide(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[ui.sidebar.agents]
rows = [
  ["state_icon", "workspace"],
  [
    { token = "agent", fg = "#ffffff", rules = [{ equals = "hide-me", hide = true }, { contains = "pi", ignore_case = true, fg = "#ff0000", bold = true }] },
    { token = "$load", rules = [{ gt = 80, fg = "#00ff00" }] },
  ],
]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	layout, errs := loadSidebarLayout(path)
	if len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
	if len(layout.AgentRows) != 2 || len(layout.AgentRows[1][0].Rules) != 2 {
		t.Fatalf("rules not parsed: %+v", layout.AgentRows)
	}
	agent, hidden := applySidebarRules(layout.AgentRows[1][0], "PI-bot")
	if hidden || agent.Fg != "#ff0000" || !agent.Bold {
		t.Fatalf("contains rule: %+v hide=%v", agent, hidden)
	}
	load, hidden := applySidebarRules(layout.AgentRows[1][1], "90")
	if hidden || load.Fg != "#00ff00" {
		t.Fatalf("gt rule: %+v hide=%v", load, hidden)
	}
	_, hidden = applySidebarRules(layout.AgentRows[1][0], "hide-me")
	if !hidden {
		t.Fatal("hide rule did not match")
	}
	plain, _ := renderSidebarRows(layout.AgentRows[1:], map[string]string{"agent": "hide-me", "$load": "90"}, "idle", colorTheme{})
	if len(plain) != 1 || plain[0] != "90" {
		t.Fatalf("hide not applied: %q", plain)
	}
	gone, _ := renderSidebarRows(layout.AgentRows[1:], map[string]string{"agent": "hide-me"}, "idle", colorTheme{})
	if len(gone) != 0 {
		t.Fatalf("hidden row remained: %q", gone)
	}
}
