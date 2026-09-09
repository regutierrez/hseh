package space

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/regutierrez/hseh/internal/herdr"
)

func preflightDirs(def Definition) error {
	info, err := os.Stat(def.ResolvedDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("hseh create %s: working directory does not exist: %s", def.ID, def.ResolvedDir)
	}
	if err := checkTabDirs(def.ResolvedDir, def.Tabs); err != nil {
		return fmt.Errorf("hseh create %s: %w", def.ID, err)
	}
	return nil
}

type pendingPaneCommand struct {
	paneID  string
	command string
}

func applyLayout(ctx context.Context, def Definition, workspaceID, rootTabID, rootPaneID string) (int, error) {
	dirs, err := resolvePaneDirs(def.ResolvedDir, def.Tabs)
	if err != nil {
		return 0, fmt.Errorf("hseh create %s: %w", def.ID, err)
	}
	var runs []pendingPaneCommand
	submitted := 0
	for i, tab := range def.Tabs {
		tabRoot := rootPaneID
		if i == 0 {
			if dir := dirs[i][0]; dir != "" && dir != def.ResolvedDir {
				_, tabRoot, err = herdr.CreateTab(ctx, workspaceID, tab.Name, dir, true)
				if err != nil {
					return submitted, fmt.Errorf("hseh create %s: tab.create %q: %w", def.ID, tab.Name, err)
				}
				if err = herdr.CloseTab(ctx, rootTabID); err != nil {
					return submitted, fmt.Errorf("hseh create %s: close replaced root tab: %w", def.ID, err)
				}
			} else if err = herdr.RenameTab(ctx, rootTabID, tab.Name); err != nil {
				return submitted, fmt.Errorf("hseh create %s: tab.rename %q: %w", def.ID, tab.Name, err)
			}
		} else {
			_, tabRoot, err = herdr.CreateTab(ctx, workspaceID, tab.Name, dirs[i][0], false)
			if err != nil {
				return submitted, fmt.Errorf("hseh create %s: tab.create %q: %w", def.ID, tab.Name, err)
			}
		}
		prev := tabRoot
		for j, pane := range tab.effectivePanes() {
			paneID := tabRoot
			if j > 0 {
				paneID, err = herdr.SplitPane(ctx, prev, pane.Split, pane.splitRatio(), dirs[i][j], false)
				if err != nil {
					return submitted, fmt.Errorf("hseh create %s: pane.split tab %q pane %d: %w", def.ID, tab.Name, j+1, err)
				}
			}
			if lbl := strings.TrimSpace(pane.Label); lbl != "" {
				if err := herdr.RenamePane(ctx, paneID, lbl); err != nil {
					return submitted, fmt.Errorf("hseh create %s: pane.rename tab %q pane %d: %w", def.ID, tab.Name, j+1, err)
				}
			}
			if strings.TrimSpace(pane.Command) != "" {
				runs = append(runs, pendingPaneCommand{paneID: paneID, command: pane.Command})
			}
			prev = paneID
		}
	}
	for _, run := range runs {
		if err := herdr.SubmitPaneCommand(ctx, run.paneID, run.command); err != nil {
			return submitted, fmt.Errorf("hseh create %s: pane.send_input %s: %w", def.ID, run.paneID, err)
		}
		submitted++
	}
	return submitted, nil
}
