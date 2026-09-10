package picker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
)

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
	}, focus.EmptyHistory(herdr.ContinuityWitness{}), layout, nil)
	if strings.Contains(strings.Join(items[0].Rows, ""), "\x1b") {
		t.Fatalf("JSON rows styled: %q", items[0].Rows)
	}
	if len(items[0].Rows) != 3 || items[0].Rows[2] != "alpha" {
		t.Fatalf("plain rows %q", items[0].Rows)
	}
}
