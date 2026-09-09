package main

import (
	"os"
	"strings"
)

type pickerLaunchContext struct {
	FromAgentPane bool
	CurrentSpace  string
	CurrentAgent  string
}

func pickerLaunchFromEnv(snapshot HerdrSessionSnapshot, history FocusHistory) pickerLaunchContext {
	ctx := pickerLaunchContext{
		CurrentSpace: snapshot.FocusedWorkspaceID,
	}
	if snapshot.FocusedPaneID != "" {
		if id, ok := currentAgentLiveID(history, snapshot.FocusedPaneID); ok {
			ctx.CurrentAgent = id.String()
		}
		for _, agent := range snapshot.Agents {
			if agent.PaneID == snapshot.FocusedPaneID {
				ctx.FromAgentPane = true
				break
			}
		}
	}
	if os.Getenv("HERDR_PLUGIN_CONTEXT_JSON") != "" {
		// focused_pane_agent in context is enough to treat launch as from an agent pane.
		if !ctx.FromAgentPane {
			ctx.FromAgentPane = containsAgentContextJSON(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"))
		}
	}
	return ctx
}

func containsAgentContextJSON(raw string) bool {
	if strings.Contains(raw, `"focused_pane_agent":null`) || strings.Contains(raw, `"focused_pane_agent":""`) {
		return false
	}
	return strings.Contains(raw, `"focused_pane_agent":"`)
}

// PreselectPickerItemID chooses the previous target, then first non-current, then current.
func PreselectPickerItemID(view string, items []PickerItem, history FocusHistory, launch pickerLaunchContext) string {
	if len(items) == 0 {
		return ""
	}
	wanted := previousTargetID(view, items, history, launch)
	if wanted != "" && pickerItemByID(items, wanted) {
		return wanted
	}
	for _, item := range items {
		if !isCurrentPickerItem(item, launch) {
			return item.ID
		}
	}
	return items[0].ID
}

func previousTargetID(view string, items []PickerItem, history FocusHistory, launch pickerLaunchContext) string {
	switch view {
	case pickerViewAgents:
		return previousAgentID(history, launch.CurrentAgent, items)
	default:
		return previousSpaceID(history, launch.CurrentSpace, items)
	}
}

func pickerItemTarget(item PickerItem) string {
	switch item.Kind {
	case pickerKindSpace:
		return item.WorkspaceID
	case pickerKindDefinition:
		return item.DefinitionID
	case pickerKindAgent:
		return strings.TrimPrefix(item.ID, pickerKindAgent+":")
	default:
		return item.ID
	}
}

func previousSpaceID(history FocusHistory, current string, items []PickerItem) string {
	for _, id := range history.Spaces {
		if id == current {
			continue
		}
		for _, item := range items {
			if item.Kind == pickerKindSpace && item.WorkspaceID == id {
				return item.ID
			}
		}
	}
	return ""
}

func previousAgentID(history FocusHistory, current string, items []PickerItem) string {
	for _, id := range history.Agents {
		s := id.String()
		if s == current {
			continue
		}
		for _, item := range items {
			if item.Kind == pickerKindAgent && pickerItemTarget(item) == s {
				return item.ID
			}
		}
	}
	return ""
}

func isCurrentPickerItem(item PickerItem, launch pickerLaunchContext) bool {
	switch item.Kind {
	case pickerKindSpace:
		return item.WorkspaceID == launch.CurrentSpace || pickerItemTarget(item) == launch.CurrentSpace
	case pickerKindAgent:
		return pickerItemTarget(item) == launch.CurrentAgent
	default:
		return false
	}
}

func pickerItemByID(items []PickerItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
