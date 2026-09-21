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

// configDir is Herdr's managed plugin config, the same directory plugin
// popup/action commands receive as HERDR_PLUGIN_CONFIG_DIR.
func configDir() string {
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(xdgConfigHome(), "herdr", "plugins", "config", PluginID())
}

func SpaceDefinitionsDir() string {
	return filepath.Join(configDir(), "spaces")
}

// SessionName is the Herdr session hseh runs in: HERDR_SESSION, else the
// /sessions/<name>/ segment of the socket path, else "default".
func SessionName() string {
	if name := os.Getenv("HERDR_SESSION"); name != "" {
		return name
	}
	const marker = "/sessions/"
	socket := SocketPath()
	if index := strings.Index(socket, marker); index >= 0 {
		rest := socket[index+len(marker):]
		if slash := strings.Index(rest, "/"); slash > 0 {
			return rest[:slash]
		}
	}
	return "default"
}

// sessionNamespaceKey names the per-session state subdirectory. Without
// HERDR_SESSION the whole socket path is used so unrelated sessions never share state.
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
	return filepath.Join(xdgConfigHome(), "herdr", "config.toml")
}

func SocketPath() string {
	return os.Getenv("HERDR_SOCKET_PATH")
}
