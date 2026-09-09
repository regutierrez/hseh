package main

import (
	"strings"
	"testing"
)

func TestOraclePreviewDoesNotEraseSelection(t *testing.T) {
	m := pickerModel{width: 60, height: 22, widePreviewMinCols: 40, selectedID: "w2", visible: []PickerItem{
		{ID: "w1", Rows: []string{"alpha"}, DisplayRows: []string{"alpha"}},
		{ID: "w2", Rows: []string{"beta"}, DisplayRows: []string{"beta"}},
	}}
	before := m.View()
	m.previewText = strings.Repeat("BETA_TICK_1\n", 15)
	after := m.View()
	if !hasRailSelectedLabel(before, "beta") || !hasRailSelectedLabel(after, "beta") {
		t.Fatalf("selection before=%q after=%q", before, after)
	}
}

func TestOracleStalePreviewReplyKeepsActiveRead(t *testing.T) {
	m := pickerModel{previewInFlight: true, previewSeq: 2, widePreviewMinCols: 80, width: 100}
	next, _ := m.Update(previewLoadedMsg{seq: 1})
	got := next.(pickerModel)
	if !got.previewInFlight {
		t.Fatal("stale cancelled reply cleared newer in-flight read guard")
	}
}
