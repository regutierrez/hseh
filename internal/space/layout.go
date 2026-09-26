package space

import (
	"context"
	"fmt"
	"strings"

	"github.com/regutierrez/hseh/internal/herdr"
)

type pendingPaneCommand struct {
	paneID  string
	command string
}

// applyLayout builds the definition's tabs and panes inside a freshly created workspace.
// dirs is resolvePaneDirs output, checked before the workspace was created. Startup
// commands are submitted last, once every pane exists.
func applyLayout(ctx context.Context, def Definition, dirs [][]string, workspaceID, rootTabID, rootPaneID string) error {
	var runs []pendingPaneCommand
	var err error
	for i, tab := range def.Tabs {
		tabRoot := rootPaneID
		if i == 0 {
			if dir := dirs[i][0]; dir != def.ResolvedDir {
				_, tabRoot, err = herdr.CreateTab(ctx, workspaceID, tab.Name, dir, true)
				if err != nil {
					return fmt.Errorf("hseh create %s: tab.create %q: %w", def.ID, tab.Name, err)
				}
				if err = herdr.CloseTab(ctx, rootTabID); err != nil {
					return fmt.Errorf("hseh create %s: close replaced root tab: %w", def.ID, err)
				}
			} else if err = herdr.RenameTab(ctx, rootTabID, tab.Name); err != nil {
				return fmt.Errorf("hseh create %s: tab.rename %q: %w", def.ID, tab.Name, err)
			}
		} else {
			_, tabRoot, err = herdr.CreateTab(ctx, workspaceID, tab.Name, dirs[i][0], false)
			if err != nil {
				return fmt.Errorf("hseh create %s: tab.create %q: %w", def.ID, tab.Name, err)
			}
		}
		prev := tabRoot
		for j, pane := range tab.effectivePanes() {
			paneID := tabRoot
			if j > 0 {
				paneID, err = herdr.SplitPane(ctx, prev, pane.Split, pane.splitRatio(), dirs[i][j], false)
				if err != nil {
					return fmt.Errorf("hseh create %s: pane.split tab %q pane %d: %w", def.ID, tab.Name, j+1, err)
				}
			}
			if lbl := strings.TrimSpace(pane.Label); lbl != "" {
				if err := herdr.RenamePane(ctx, paneID, lbl); err != nil {
					return fmt.Errorf("hseh create %s: pane.rename tab %q pane %d: %w", def.ID, tab.Name, j+1, err)
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
			return fmt.Errorf("hseh create %s: pane.send_input %s: %w", def.ID, run.paneID, err)
		}
	}
	return nil
}
