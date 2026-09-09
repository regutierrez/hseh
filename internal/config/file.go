package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultPreviewPollMilliseconds is the selected visible preview poll interval.
const DefaultPreviewPollMilliseconds = 500

// DefaultWidePreviewMinColumns is the approved popup content width that shows the preview.
const DefaultWidePreviewMinColumns = 100

type File struct {
	PreviewPollMs         int    `toml:"preview_poll_ms"`
	WidePreviewMinColumns int    `toml:"wide_preview_min_columns"`
	TraceFile             string `toml:"trace_file"`
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
