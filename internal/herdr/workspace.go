package herdr

import (
	"context"
	"fmt"
	"strings"
)

type workspaceCreatedEnvelope struct {
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

type tabCreatedEnvelope struct {
	Type string `json:"type"`
	Tab  struct {
		TabID string `json:"tab_id"`
	} `json:"tab"`
	RootPane struct {
		PaneID string `json:"pane_id"`
	} `json:"root_pane"`
}

type paneSplitEnvelope struct {
	Type string `json:"type"`
	Pane struct {
		PaneID string `json:"pane_id"`
	} `json:"pane"`
}

func CreateWorkspace(ctx context.Context, cwd, label string, focus bool) (workspaceID, tabID, paneID string, err error) {
	var out workspaceCreatedEnvelope
	_, err = callContext(ctx, "workspace.create", map[string]any{
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

func CreateTab(ctx context.Context, workspaceID, label, cwd string, focus bool) (tabID, paneID string, err error) {
	params := map[string]any{
		"workspace_id": workspaceID,
		"label":        label,
		"focus":        focus,
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	var out tabCreatedEnvelope
	_, err = callContext(ctx, "tab.create", params, &out)
	if err != nil {
		return "", "", err
	}
	if out.Tab.TabID == "" || out.RootPane.PaneID == "" {
		return "", "", fmt.Errorf("hseh create: tab.create missing ids")
	}
	return out.Tab.TabID, out.RootPane.PaneID, nil
}

func RenameTab(ctx context.Context, tabID, label string) error {
	_, err := callContext(ctx, "tab.rename", map[string]any{"tab_id": tabID, "label": label}, nil)
	return err
}

func CloseTab(ctx context.Context, tabID string) error {
	_, err := callContext(ctx, "tab.close", map[string]any{"tab_id": tabID}, nil)
	return err
}

func SplitPane(ctx context.Context, targetPaneID, direction string, ratio float64, cwd string, focus bool) (string, error) {
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
	var out paneSplitEnvelope
	_, err := callContext(ctx, "pane.split", params, &out)
	if err != nil {
		return "", err
	}
	if out.Pane.PaneID == "" {
		return "", fmt.Errorf("hseh create: pane.split missing pane_id")
	}
	return out.Pane.PaneID, nil
}

func RenamePane(ctx context.Context, paneID, label string) error {
	_, err := callContext(ctx, "pane.rename", map[string]any{"pane_id": paneID, "label": label}, nil)
	return err
}

func SubmitPaneCommand(ctx context.Context, paneID, command string) error {
	_, err := callContext(ctx, "pane.send_input", map[string]any{
		"pane_id": paneID,
		"text":    command,
		"keys":    []string{"Enter"},
	}, nil)
	return err
}
