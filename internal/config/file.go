package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultPreviewPollMilliseconds is the selected visible preview poll interval.
const DefaultPreviewPollMilliseconds = 500

// DefaultWidePreviewMinColumns is the popup content width at which the preview sits beside the list.
const DefaultWidePreviewMinColumns = 100

// Default popup geometry, matching the [[panes]] entries in herdr-plugin.toml.
var (
	DefaultPopupWidth  = PopupSize{Percent: 85}
	DefaultPopupHeight = PopupSize{Percent: 80}
)

// Settings is hseh.toml with defaults applied. Every key is optional; an invalid
// value is reported and that key falls back to its default.
type Settings struct {
	PreviewPoll           time.Duration
	WidePreviewMinColumns int
	PopupWidth            PopupSize
	PopupHeight           PopupSize
	TraceFile             string
}

type file struct {
	PreviewPollMs         int    `toml:"preview_poll_ms"`
	WidePreviewMinColumns int    `toml:"wide_preview_min_columns"`
	TraceFile             string `toml:"trace_file"`
	// PopupWidth and PopupHeight take a percentage string ("70%") or a cell
	// count (120), the two forms Herdr's plugin.pane.open accepts.
	PopupWidth  any `toml:"popup_width"`
	PopupHeight any `toml:"popup_height"`
}

// PopupSize is one popup dimension: exactly one of Percent or Cells is set.
type PopupSize struct {
	Percent int
	Cells   int
}

// Param is the value Herdr expects in plugin.pane.open: a "N%" string for
// percentages, a bare number for cells.
func (s PopupSize) Param() any {
	if s.Cells > 0 {
		return s.Cells
	}
	return fmt.Sprintf("%d%%", s.Percent)
}

func parsePopupSize(key string, raw any) (PopupSize, error) {
	switch v := raw.(type) {
	case string:
		text := strings.TrimSpace(v)
		if !strings.HasSuffix(text, "%") {
			return PopupSize{}, fmt.Errorf("hseh config: %s %q must be a percentage like \"80%%\" or a cell count like 120", key, v)
		}
		percent, err := strconv.Atoi(strings.TrimSuffix(text, "%"))
		if err != nil || percent < 1 || percent > 100 {
			return PopupSize{}, fmt.Errorf("hseh config: %s %q must be between 1%% and 100%%", key, v)
		}
		return PopupSize{Percent: percent}, nil
	case int64:
		if v < 1 {
			return PopupSize{}, fmt.Errorf("hseh config: %s %d must be a positive cell count", key, v)
		}
		return PopupSize{Cells: int(v)}, nil
	default:
		return PopupSize{}, fmt.Errorf("hseh config: %s must be a percentage like \"80%%\" or a cell count like 120", key)
	}
}

// Load reads hseh.toml from the plugin config directory. A missing file is the defaults.
func Load() (Settings, []string) {
	settings := Settings{
		PreviewPoll:           time.Duration(DefaultPreviewPollMilliseconds) * time.Millisecond,
		WidePreviewMinColumns: DefaultWidePreviewMinColumns,
		PopupWidth:            DefaultPopupWidth,
		PopupHeight:           DefaultPopupHeight,
	}
	path := filepath.Join(ConfigDir(), "hseh.toml")
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return settings, nil
		}
		return settings, []string{fmt.Sprintf("hseh config: read %s: %v", path, err)}
	}
	var f file
	if err := toml.Unmarshal(payload, &f); err != nil {
		return settings, []string{fmt.Sprintf("hseh config: parse %s: %v", path, err)}
	}
	var errs []string
	if f.PreviewPollMs > 0 {
		settings.PreviewPoll = time.Duration(f.PreviewPollMs) * time.Millisecond
	}
	if f.WidePreviewMinColumns > 0 {
		settings.WidePreviewMinColumns = f.WidePreviewMinColumns
	}
	settings.TraceFile = f.TraceFile
	if f.PopupWidth != nil {
		if parsed, err := parsePopupSize("popup_width", f.PopupWidth); err != nil {
			errs = append(errs, err.Error())
		} else {
			settings.PopupWidth = parsed
		}
	}
	if f.PopupHeight != nil {
		if parsed, err := parsePopupSize("popup_height", f.PopupHeight); err != nil {
			errs = append(errs, err.Error())
		} else {
			settings.PopupHeight = parsed
		}
	}
	return settings, errs
}
