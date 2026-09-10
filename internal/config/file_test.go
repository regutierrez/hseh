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

func TestLoadDefaultsWithoutFile(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", t.TempDir())
	settings, errs := Load()
	if len(errs) != 0 {
		t.Fatalf("errs %v", errs)
	}
	if settings.PopupWidth.Param() != "85%" || settings.PopupHeight.Param() != "80%" {
		t.Fatalf("popup defaults width=%v height=%v", settings.PopupWidth.Param(), settings.PopupHeight.Param())
	}
	if settings.WidePreviewMinColumns != DefaultWidePreviewMinColumns || settings.PreviewPoll.Milliseconds() != DefaultPreviewPollMilliseconds {
		t.Fatalf("defaults %+v", settings)
	}
}

func TestLoadReadsEveryKey(t *testing.T) {
	writeConfig(t, "popup_width = \"70%\"\npopup_height = 30\nwide_preview_min_columns = 40\npreview_poll_ms = 250\ntrace_file = \"/tmp/t\"\n")
	settings, errs := Load()
	if len(errs) != 0 {
		t.Fatalf("errs %v", errs)
	}
	if settings.PopupWidth.Param() != "70%" || settings.PopupHeight.Param() != 30 {
		t.Fatalf("popup %#v %#v", settings.PopupWidth.Param(), settings.PopupHeight.Param())
	}
	if settings.WidePreviewMinColumns != 40 || settings.PreviewPoll.Milliseconds() != 250 || settings.TraceFile != "/tmp/t" {
		t.Fatalf("settings %+v", settings)
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
		settings, errs := Load()
		if len(errs) != 1 || !strings.Contains(errs[0], want) {
			t.Fatalf("%q: errs %v, want one mentioning %q", body, errs, want)
		}
		if settings.PopupWidth != DefaultPopupWidth {
			t.Fatalf("%q: invalid width did not fall back: %v", body, settings.PopupWidth)
		}
		if settings.PopupHeight.Param() != "50%" {
			t.Fatalf("%q: valid height lost: %v", body, settings.PopupHeight.Param())
		}
	}
}
