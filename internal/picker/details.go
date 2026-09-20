package picker

import (
	"os"
	"path/filepath"
	"strings"
)

// mutedSGR is the default-theme (catppuccin) subtext0 SGR. Tests that build
// DisplayRows without a live theme, and rows assembled with an empty theme,
// use this token instead of a hardcoded 256-color gray.
var mutedSGR = hexSGR("#a6adc8", false)

// Nerd Fonts 3 glyph: nf-dev-git_branch.
const gitBranchIcon = "\ue725"

func statePrefix(status, mode string, th colorTheme) (plain, display string) {
	if status == "" {
		return "", ""
	}
	icon := stateIconGlyph(status, mode)
	return icon + " ", stylePlainToken(sidebarToken{Name: "state_icon"}, icon, status, th) + " "
}

func agentHarnessIcon(agent string) string {
	switch agent {
	case "pi", "omp":
		return "\U000f03ff" // nf-md-pi
	case "claude", "claude-code":
		return "\uec82" // nf-cod-claude, Nerd Fonts 3.5+
	case "codex":
		return "\uec81" // nf-cod-openai, Nerd Fonts 3.5+
	case "amp":
		return "amp"
	case "gemini":
		return "✦"
	case "grok":
		return "grok"
	default:
		return "\U000f018d" // nf-md-console
	}
}

func abbreviatedDirectory(dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	home, _ := os.UserHomeDir()
	if home != "" && dir == home {
		return "~"
	}
	if home != "" && strings.HasPrefix(dir, home+string(filepath.Separator)) {
		relative := strings.TrimPrefix(dir, home+string(filepath.Separator))
		if filepath.Dir(relative) == "." {
			return "~/" + relative
		}
		return "~/…/" + filepath.Base(dir)
	}
	if dir == string(filepath.Separator) {
		return dir
	}
	if filepath.IsAbs(dir) {
		if filepath.Dir(dir) == string(filepath.Separator) {
			return dir
		}
		return "/…/" + filepath.Base(dir)
	}
	return dir
}
