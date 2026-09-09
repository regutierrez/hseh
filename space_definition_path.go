package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// expandSpaceDefinitionPath copies Herdr Plus expandPath: empty or ~ is home,
// ~/ joins home, $VARS expand, then Clean. See third_party/herdr-plus/NOTICE.
func expandSpaceDefinitionPath(s string) (string, error) {
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

// resolveSpaceDefinitionNestedDir copies Herdr Plus resolveNestedDir: empty inherits,
// relative paths join root. See third_party/herdr-plus/NOTICE.
func resolveSpaceDefinitionNestedDir(raw, root string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	dir, err := expandSpaceDefinitionPath(raw)
	if err != nil {
		return "", fmt.Errorf("resolve working_dir %q: %w", raw, err)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return dir, nil
}

// effectivePanes copies Herdr Plus tab.effectivePanes. See third_party/herdr-plus/NOTICE.
func (t SpaceDefinitionTab) effectivePanes() []SpaceDefinitionPane {
	if len(t.Panes) == 0 {
		return []SpaceDefinitionPane{{Command: t.Command, WorkingDir: t.WorkingDir}}
	}
	panes := make([]SpaceDefinitionPane, len(t.Panes))
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
			panes[i].Split = spaceDefinitionSplitDown
		}
	}
	return panes
}

// splitRatio copies Herdr Plus pane.splitRatio: omitted/zero ratio is an even split
// and is left out of pane.split; a positive authored share becomes 1-share for Herdr.
// See third_party/herdr-plus/NOTICE.
func (p SpaceDefinitionPane) splitRatio() float64 {
	if p.Ratio <= 0 {
		return 0
	}
	return 1 - p.Ratio
}

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

func resolveSpaceDefinitionPaneDirs(root string, tabs []SpaceDefinitionTab) ([][]string, error) {
	dirs := make([][]string, len(tabs))
	for i, tab := range tabs {
		panes := tab.effectivePanes()
		dirs[i] = make([]string, len(panes))
		for j, pane := range panes {
			dir, err := resolveSpaceDefinitionNestedDir(pane.WorkingDir, root)
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

func checkSpaceDefinitionTabDirs(root string, tabs []SpaceDefinitionTab) error {
	_, err := resolveSpaceDefinitionPaneDirs(root, tabs)
	return err
}
