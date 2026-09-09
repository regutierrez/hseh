package termtext

import (
	"strings"
	"testing"
)

func TestAllowVisiblePreviewANSIKeepsSGRDropsOSC(t *testing.T) {
	input := "ok\x1b[32mgreen\x1b[0m\x1b]52;c;SECRET\x07\x1b[2J\x1b[Hdone"
	got := KeepSGR(input)
	if got != "ok\x1b[32mgreen\x1b[0mdone" {
		t.Fatalf("got %q", got)
	}
}

func TestAllowVisiblePreviewANSIDropsClipboardBEL(t *testing.T) {
	got := KeepSGR("x\x07y")
	if got != "xy" {
		t.Fatalf("got %q", got)
	}
}

func TestAllowVisiblePreviewANSIDropsC1AndInvalidBytes(t *testing.T) {
	input := "text\u009b2J\u009d52;c;YQ==\u009ctail\x1b"
	got := KeepSGR(input)
	for _, r := range got {
		if r >= 0x80 && r <= 0x9f {
			t.Fatalf("C1 survives U+%04X in %q", r, got)
		}
	}
	if strings.Contains(got, "\x1b") && !strings.Contains(got, "\x1b[") {
		t.Fatalf("incomplete ESC survived: %q", got)
	}
}

func TestAllowVisiblePreviewANSINormalizesCRLFDropsBareCR(t *testing.T) {
	got := KeepSGR("a\r\nb\rc\t")
	if strings.ContainsRune(got, '\r') {
		t.Fatalf("CR survived: %q", got)
	}
	if got != "a\nbc " {
		t.Fatalf("got %q", got)
	}
}

func TestStripTerminalControlsRemovesSGR(t *testing.T) {
	got := StripControls("a\x1b[31mx\x1b[0mb")
	if got != "axb" {
		t.Fatalf("got %q", got)
	}
}
