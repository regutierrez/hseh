package space

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Empty or ~ is home, ~/ joins home, $VARS expand, then Clean.
func expandPath(s string) (string, error) {
	dir := strings.TrimSpace(s)
	home, err := os.UserHomeDir()
	needsHome := dir == "" || dir == "~" || strings.HasPrefix(dir, "~/")
	if err != nil && needsHome {
		return "", err
	}
	if dir == "" || dir == "~" {
		return home, nil
	}
	if strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(home, dir[2:])
	}
	return filepath.Clean(os.ExpandEnv(dir)), nil
}

// Empty inherits; relative paths join root.
func resolveNestedDir(raw, root string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	dir, err := expandPath(raw)
	if err != nil {
		return "", fmt.Errorf("resolve working_dir %q: %w", raw, err)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return dir, nil
}

func (t DefinitionTab) effectivePanes() []DefinitionPane {
	if len(t.Panes) == 0 {
		return []DefinitionPane{{Command: t.Command, WorkingDir: t.WorkingDir}}
	}
	panes := make([]DefinitionPane, len(t.Panes))
	for i, pane := range t.Panes {
		panes[i] = pane
		if strings.TrimSpace(panes[i].WorkingDir) == "" {
			panes[i].WorkingDir = t.WorkingDir
		}
		if i == 0 {
			panes[i].Split = ""
			panes[i].Ratio = 0
			continue
		}
		if panes[i].Split == "" {
			panes[i].Split = splitDown
		}
	}
	return panes
}

// Omitted/zero ratio is an even split and is left out of pane.split;
// a positive authored share becomes 1-share for Herdr.
func (p DefinitionPane) splitRatio() float64 {
	if p.Ratio <= 0 {
		return 0
	}
	return 1 - p.Ratio
}

// canonicalizeDirPath is the one path identity rule: absolute, symlinks resolved when the
// path exists, cleaned otherwise.
func canonicalizeDirPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("directory is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return filepath.Clean(abs), nil
}

func requireExistingDir(path string) (string, error) {
	canonical, err := canonicalizeDirPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("working directory does not exist: %s", path)
	}
	return canonical, nil
}

func resolvePaneDirs(root string, tabs []DefinitionTab) ([][]string, error) {
	dirs := make([][]string, len(tabs))
	for i, tab := range tabs {
		panes := tab.effectivePanes()
		dirs[i] = make([]string, len(panes))
		for j, pane := range panes {
			dir, err := resolveNestedDir(pane.WorkingDir, root)
			if err != nil {
				return nil, fmt.Errorf("tab %q pane %d: %w", tab.Name, j+1, err)
			}
			if dir == "" {
				dirs[i][j] = root
				continue
			}
			abs, err := requireExistingDir(dir)
			if err != nil {
				return nil, fmt.Errorf("tab %q pane %d: %w", tab.Name, j+1, err)
			}
			dirs[i][j] = abs
		}
	}
	return dirs, nil
}
