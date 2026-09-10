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

// DefaultWidePreviewMinColumns is the approved popup content width that shows the preview.
const DefaultWidePreviewMinColumns = 100

// Default popup geometry, matching the [[panes]] entries in herdr-plugin.toml.
var (
	DefaultPopupWidth  = PopupSize{Percent: 85}
	DefaultPopupHeight = PopupSize{Percent: 80}
)

type File struct {
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

func (s PopupSize) String() string { return fmt.Sprint(s.Param()) }

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

// LoadPopupSize reads popup_width and popup_height from plugin config. Each
// dimension falls back to its default independently when unset or invalid.
func LoadPopupSize() (width, height PopupSize, errs []string) {
	width, height = DefaultPopupWidth, DefaultPopupHeight
	file, errs := LoadFile()
	if len(errs) > 0 {
		return width, height, errs
	}
	if file.PopupWidth != nil {
		if parsed, err := parsePopupSize("popup_width", file.PopupWidth); err != nil {
			errs = append(errs, err.Error())
		} else {
			width = parsed
		}
	}
	if file.PopupHeight != nil {
		if parsed, err := parsePopupSize("popup_height", file.PopupHeight); err != nil {
			errs = append(errs, err.Error())
		} else {
			height = parsed
		}
	}
	return width, height, errs
}

func LoadFile() (File, []string) {
	path := filepath.Join(ConfigDir(), "hseh.toml")
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return File{}, []string{fmt.Sprintf("hseh config: read %s: %v", path, err)}
	}
	var file File
	if err := toml.Unmarshal(payload, &file); err != nil {
		return File{}, []string{fmt.Sprintf("hseh config: parse %s: %v", path, err)}
	}
	return file, nil
}

// LoadPreviewPollInterval reads preview_poll_ms from plugin config, else 500ms.
func LoadPreviewPollInterval() (time.Duration, []string) {
	file, errs := LoadFile()
	if len(errs) > 0 {
		return time.Duration(DefaultPreviewPollMilliseconds) * time.Millisecond, errs
	}
	if file.PreviewPollMs <= 0 {
		return time.Duration(DefaultPreviewPollMilliseconds) * time.Millisecond, nil
	}
	return time.Duration(file.PreviewPollMs) * time.Millisecond, nil
}

func LoadWidePreviewMinColumns() (int, []string) {
	file, errs := LoadFile()
	if len(errs) > 0 {
		return DefaultWidePreviewMinColumns, errs
	}
	if file.WidePreviewMinColumns <= 0 {
		return DefaultWidePreviewMinColumns, nil
	}
	return file.WidePreviewMinColumns, nil
}
