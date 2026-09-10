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
[ui.sidebar.spaces]
rows = [[{ token = "workspace", fg = "#89b4fa", bold = true }]]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	layout, errs := LoadSidebarLayout(path)
	if len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
	if len(layout.SpaceRows) != 1 || !layout.SpaceRows[0][0].Styled || layout.SpaceRows[0][0].Fg != "#89b4fa" {
		t.Fatalf("style dropped: %+v", layout.SpaceRows)
	}
	items := BuildItemsWithLayout("spaces", herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "alpha"}},
	}, focus.EmptyHistory(herdr.ContinuityWitness{}), layout)
	if strings.Contains(strings.Join(items[0].Rows, ""), "\x1b") {
		t.Fatalf("JSON rows styled: %q", items[0].Rows)
	}
	if row := items[0].Rows[0]; !strings.HasSuffix(row, " alpha") || !strings.Contains(row, " herdr") {
		t.Fatalf("plain row %q", items[0].Rows)
	}
}
