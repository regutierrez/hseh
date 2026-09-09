package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type herdrWorkspaceCreated struct {
	Type      string `json:"type"`
	Workspace struct {
		WorkspaceID string `json:"workspace_id"`
	} `json:"workspace"`
	Tab struct {
		TabID string `json:"tab_id"`
	} `json:"tab"`
	RootPane struct {
		PaneID string `json:"pane_id"`
	} `json:"root_pane"`
}

type herdrTabCreated struct {
	Type string `json:"type"`
	Tab  struct {
		TabID string `json:"tab_id"`
	} `json:"tab"`
	RootPane struct {
		PaneID string `json:"pane_id"`
	} `json:"root_pane"`
}

type herdrPaneSplitResult struct {
	Type string `json:"type"`
	Pane struct {
		PaneID string `json:"pane_id"`
	} `json:"pane"`
}

func createHerdrWorkspace(ctx context.Context, cwd, label string, focus bool) (workspaceID, tabID, paneID string, err error) {
	var out herdrWorkspaceCreated
	_, err = CallHerdrMethodContext(ctx, "workspace.create", map[string]any{
		"cwd":   cwd,
		"label": label,
		"focus": focus,
	}, &out)
	if err != nil {
		return "", "", "", err
	}
	if out.Workspace.WorkspaceID == "" || out.Tab.TabID == "" || out.RootPane.PaneID == "" {
		return "", "", "", fmt.Errorf("hseh create: workspace.create missing ids")
	}
	return out.Workspace.WorkspaceID, out.Tab.TabID, out.RootPane.PaneID, nil
}

func createHerdrTab(ctx context.Context, workspaceID, label, cwd string, focus bool) (tabID, paneID string, err error) {
	params := map[string]any{
		"workspace_id": workspaceID,
		"label":        label,
		"focus":        focus,
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	var out herdrTabCreated
	_, err = CallHerdrMethodContext(ctx, "tab.create", params, &out)
	if err != nil {
		return "", "", err
	}
	if out.Tab.TabID == "" || out.RootPane.PaneID == "" {
		return "", "", fmt.Errorf("hseh create: tab.create missing ids")
	}
	return out.Tab.TabID, out.RootPane.PaneID, nil
}

func renameHerdrTab(ctx context.Context, tabID, label string) error {
	var result map[string]any
	_, err := CallHerdrMethodContext(ctx, "tab.rename", map[string]any{"tab_id": tabID, "label": label}, &result)
	return err
}

func closeHerdrTab(ctx context.Context, tabID string) error {
	var result map[string]any
	_, err := CallHerdrMethodContext(ctx, "tab.close", map[string]any{"tab_id": tabID}, &result)
	return err
}

func splitHerdrPane(ctx context.Context, targetPaneID, direction string, ratio float64, cwd string, focus bool) (string, error) {
	params := map[string]any{
		"target_pane_id": targetPaneID,
		"direction":      direction,
		"focus":          focus,
	}
	if ratio > 0 {
		params["ratio"] = ratio
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	var out herdrPaneSplitResult
	_, err := CallHerdrMethodContext(ctx, "pane.split", params, &out)
	if err != nil {
		return "", err
	}
	if out.Pane.PaneID == "" {
		return "", fmt.Errorf("hseh create: pane.split missing pane_id")
	}
	return out.Pane.PaneID, nil
}

func renameHerdrPane(ctx context.Context, paneID, label string) error {
	var result map[string]any
	_, err := CallHerdrMethodContext(ctx, "pane.rename", map[string]any{"pane_id": paneID, "label": label}, &result)
	return err
}

func submitHerdrPaneCommand(ctx context.Context, paneID, command string) error {
	var result map[string]any
	_, err := CallHerdrMethodContext(ctx, "pane.send_input", map[string]any{
		"pane_id": paneID,
		"text":    command,
		"keys":    []string{"Enter"},
	}, &result)
	return err
}

func preflightSpaceDefinitionDirs(def SpaceDefinition) error {
	info, err := os.Stat(def.ResolvedDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("hseh create %s: working directory does not exist: %s", def.ID, def.ResolvedDir)
	}
	if err := checkSpaceDefinitionTabDirs(def.ResolvedDir, def.Tabs); err != nil {
		return fmt.Errorf("hseh create %s: %w", def.ID, err)
	}
	return nil
}

type pendingPaneCommand struct {
	paneID  string
	command string
}

func layoutReusableSpace(ctx context.Context, def SpaceDefinition, workspaceID, rootTabID, rootPaneID string) (int, error) {
	dirs, err := resolveSpaceDefinitionPaneDirs(def.ResolvedDir, def.Tabs)
	if err != nil {
		return 0, fmt.Errorf("hseh create %s: %w", def.ID, err)
	}
	var runs []pendingPaneCommand
	submitted := 0
	for i, tab := range def.Tabs {
		tabRoot := rootPaneID
		if i == 0 {
			if dir := dirs[i][0]; dir != "" && dir != def.ResolvedDir {
				_, tabRoot, err = createHerdrTab(ctx, workspaceID, tab.Name, dir, true)
				if err != nil {
					return submitted, fmt.Errorf("hseh create %s: tab.create %q: %w", def.ID, tab.Name, err)
				}
				if err = closeHerdrTab(ctx, rootTabID); err != nil {
					return submitted, fmt.Errorf("hseh create %s: close replaced root tab: %w", def.ID, err)
				}
			} else if err = renameHerdrTab(ctx, rootTabID, tab.Name); err != nil {
				return submitted, fmt.Errorf("hseh create %s: tab.rename %q: %w", def.ID, tab.Name, err)
			}
		} else {
			_, tabRoot, err = createHerdrTab(ctx, workspaceID, tab.Name, dirs[i][0], false)
			if err != nil {
				return submitted, fmt.Errorf("hseh create %s: tab.create %q: %w", def.ID, tab.Name, err)
			}
		}
		prev := tabRoot
		for j, pane := range tab.effectivePanes() {
			paneID := tabRoot
			if j > 0 {
				paneID, err = splitHerdrPane(ctx, prev, pane.Split, pane.splitRatio(), dirs[i][j], false)
				if err != nil {
					return submitted, fmt.Errorf("hseh create %s: pane.split tab %q pane %d: %w", def.ID, tab.Name, j+1, err)
				}
			}
			if lbl := strings.TrimSpace(pane.Label); lbl != "" {
				if err := renameHerdrPane(ctx, paneID, lbl); err != nil {
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
		if err := submitHerdrPaneCommand(ctx, run.paneID, run.command); err != nil {
			return submitted, fmt.Errorf("hseh create %s: pane.send_input %s: %w", def.ID, run.paneID, err)
		}
		submitted++
	}
	return submitted, nil
}
