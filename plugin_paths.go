package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"
)

// DefaultPreviewPollMilliseconds is the selected visible preview poll interval.
const DefaultPreviewPollMilliseconds = 500

// DefaultWidePreviewMinColumns is the approved popup content width that shows the preview.
const DefaultWidePreviewMinColumns = 100

func xdgConfigHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

func xdgStateHome() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state")
}

// pluginStateDir is Herdr's managed plugin state, the same directory plugin
// popup/action commands receive as HERDR_PLUGIN_STATE_DIR.
func pluginStateDir() string {
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(xdgStateHome(), "herdr", "plugins", osGetenvPluginID())
}

// pluginConfigDir is Herdr's managed plugin config, the same directory plugin
// popup/action commands receive as HERDR_PLUGIN_CONFIG_DIR.
func pluginConfigDir() string {
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(xdgConfigHome(), "herdr", "plugins", "config", osGetenvPluginID())
}

func spaceDefinitionsDir() string {
	return filepath.Join(pluginConfigDir(), "spaces")
}

func withExclusiveFileLock(lockPath string, fn func() error) error {
	return withExclusiveFileLockContext(context.Background(), lockPath, fn)
}

func retryExclusiveLockError(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}

func withExclusiveFileLockContext(ctx context.Context, lockPath string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("hseh lock: mkdir %s: %w", filepath.Dir(lockPath), err)
	}
	file, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("hseh lock: open %s: %w", lockPath, err)
	}
	defer file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !retryExclusiveLockError(err) {
			return fmt.Errorf("hseh lock: flock %s: %w", lockPath, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

func sessionNamespaceKey() string {
	if name := os.Getenv("HERDR_SESSION"); name != "" {
		return sanitizeSessionKey(name)
	}
	socket := HerdrSocketPath()
	if socket == "" {
		return "unknown-session"
	}
	return sanitizeSessionKey(socket)
}

func sanitizeSessionKey(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "session"
	}
	return b.String()
}

func sessionHistoryDir(stateDir string) string {
	return filepath.Join(stateDir, sessionNamespaceKey())
}

type hsehFileConfig struct {
	PreviewPollMs         int `toml:"preview_poll_ms"`
	WidePreviewMinColumns int `toml:"wide_preview_min_columns"`
}

func loadHsehFileConfig() (hsehFileConfig, []string) {
	path := filepath.Join(pluginConfigDir(), "hseh.toml")
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return hsehFileConfig{}, nil
		}
		return hsehFileConfig{}, []string{fmt.Sprintf("hseh config: read %s: %v", path, err)}
	}
	var file hsehFileConfig
	if err := toml.Unmarshal(payload, &file); err != nil {
		return hsehFileConfig{}, []string{fmt.Sprintf("hseh config: parse %s: %v", path, err)}
	}
	return file, nil
}

// LoadPreviewPollInterval reads preview_poll_ms from plugin config, else 500ms.
func LoadPreviewPollInterval() (time.Duration, []string) {
	file, errs := loadHsehFileConfig()
	if len(errs) > 0 {
		return time.Duration(DefaultPreviewPollMilliseconds) * time.Millisecond, errs
	}
	if file.PreviewPollMs <= 0 {
		return time.Duration(DefaultPreviewPollMilliseconds) * time.Millisecond, nil
	}
	return time.Duration(file.PreviewPollMs) * time.Millisecond, nil
}

func loadWidePreviewMinColumns() (int, []string) {
	file, errs := loadHsehFileConfig()
	if len(errs) > 0 {
		return DefaultWidePreviewMinColumns, errs
	}
	if file.WidePreviewMinColumns <= 0 {
		return DefaultWidePreviewMinColumns, nil
	}
	return file.WidePreviewMinColumns, nil
}
