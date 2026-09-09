package config

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

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

// StateDir is Herdr's managed plugin state, the same directory plugin
// popup/action commands receive as HERDR_PLUGIN_STATE_DIR.
func StateDir() string {
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(xdgStateHome(), "herdr", "plugins", PluginID())
}

// ConfigDir is Herdr's managed plugin config, the same directory plugin
// popup/action commands receive as HERDR_PLUGIN_CONFIG_DIR.
func ConfigDir() string {
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(xdgConfigHome(), "herdr", "plugins", "config", PluginID())
}

func SpaceDefinitionsDir() string {
	return filepath.Join(ConfigDir(), "spaces")
}

func sessionNamespaceKey() string {
	if name := os.Getenv("HERDR_SESSION"); name != "" {
		return sanitizeSessionKey(name)
	}
	socket := SocketPath()
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

func SessionHistoryDir(stateDir string) string {
	return filepath.Join(stateDir, sessionNamespaceKey())
}

func PluginID() string {
	if id := os.Getenv("HERDR_PLUGIN_ID"); id != "" {
		return id
	}
	return "hseh"
}

func HerdrConfigPath() string {
	if path := os.Getenv("HERDR_CONFIG_PATH"); path != "" {
		return path
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "herdr", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herdr", "config.toml")
}

// SocketPath is the current session socket. HERDR_SOCKET_PATH wins.
func SocketPath() string {
	if path := os.Getenv("HERDR_SOCKET_PATH"); path != "" {
		return path
	}
	return ""
}
