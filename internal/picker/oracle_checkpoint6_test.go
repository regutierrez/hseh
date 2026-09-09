package picker

import (
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/termtext"
)

func TestOraclePreviewCRLFCannotRewindListColumn(t *testing.T) {
	m := model{width: 60, height: 22, widePreviewMinCols: 40, selectedID: "w2", visible: []Item{
		{ID: "w1", Rows: []string{"alpha"}, DisplayRows: []string{"alpha"}},
		{ID: "w2", Rows: []string{"beta"}, DisplayRows: []string{"beta"}},
	}, previewText: termtext.KeepSGR("\x1b[0mBETA_TICK_1\r\n\x1b[0mBETA_TICK_2\r\n")}
	if got := m.View(); strings.ContainsRune(got, '\r') {
		t.Fatalf("pane CR reaches side-by-side View and rewinds list column: %q", got)
	}
}
