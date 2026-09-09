package main

import (
	"os"
	"path/filepath"
	"strings"
)

const pickerMuted = "\x1b[38;5;245m"

// Nerd Fonts 3 glyph: nf-dev-git_branch.
const pickerGitBranchIcon = "\ue725"

func pickerStatePrefix(status, mode string) (plain, display string) {
	if status == "" {
		return "", ""
	}
	icon := stateIconGlyph(status, mode)
	return icon + " ", stylePlainToken(SidebarToken{Name: "state_icon"}, icon, status) + " "
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

func abbreviatedPickerDirectory(dir string) string {
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
