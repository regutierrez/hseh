package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "hseh.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPopupSizeDefaultsMatchManifest(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", t.TempDir())
	width, height, errs := LoadPopupSize()
	if len(errs) != 0 {
		t.Fatalf("errs %v", errs)
	}
	if width.Param() != "85%" || height.Param() != "80%" {
		t.Fatalf("defaults width=%v height=%v", width.Param(), height.Param())
	}
}

func TestPopupSizeAcceptsPercentAndCells(t *testing.T) {
	writeConfig(t, "popup_width = \"70%\"\npopup_height = 30\n")
	width, height, errs := LoadPopupSize()
	if len(errs) != 0 {
		t.Fatalf("errs %v", errs)
	}
	if width.Param() != "70%" {
		t.Fatalf("width param %#v", width.Param())
	}
	if height.Param() != 30 {
		t.Fatalf("height param %#v", height.Param())
	}
}

func TestPopupSizeInvalidFallsBackPerDimension(t *testing.T) {
	cases := map[string]string{
		"popup_width = \"120\"\n":  "percentage",
		"popup_width = \"0%\"\n":   "between 1% and 100%",
		"popup_width = \"101%\"\n": "between 1% and 100%",
		"popup_width = 0\n":        "positive cell count",
		"popup_width = true\n":     "percentage",
	}
	for body, want := range cases {
		writeConfig(t, body+"popup_height = \"50%\"\n")
		width, height, errs := LoadPopupSize()
		if len(errs) != 1 || !strings.Contains(errs[0], want) {
			t.Fatalf("%q: errs %v, want one mentioning %q", body, errs, want)
		}
		if width != DefaultPopupWidth {
			t.Fatalf("%q: invalid width did not fall back: %v", body, width)
		}
		if height.Param() != "50%" {
			t.Fatalf("%q: valid height lost: %v", body, height.Param())
		}
	}
}
