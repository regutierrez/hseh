package picker

import (
	"os"
	"strings"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
)

type LaunchContext struct {
	FromAgentPane bool
	CurrentSpace  string
	CurrentAgent  string
}

func launchFromEnv(snapshot herdr.SessionSnapshot, history focus.History) LaunchContext {
	ctx := LaunchContext{
		CurrentSpace: snapshot.FocusedWorkspaceID,
	}
	if snapshot.FocusedPaneID != "" {
		if id, ok := focus.CurrentAgentLiveID(history, snapshot.FocusedPaneID); ok {
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

// PreselectItemID chooses the previous target, then first non-current, then current.
func PreselectItemID(view string, items []Item, history focus.History, launch LaunchContext) string {
	if len(items) == 0 {
		return ""
	}
	wanted := previousTargetID(view, items, history, launch)
	if wanted != "" && HasItemID(items, wanted) {
		return wanted
	}
	for _, item := range items {
		if !isCurrentItem(item, launch) {
			return item.ID
		}
	}
	return items[0].ID
}

func previousTargetID(view string, items []Item, history focus.History, launch LaunchContext) string {
	switch view {
	case ViewAgents:
		return previousAgentID(history, launch.CurrentAgent, items)
	default:
		return previousSpaceID(history, launch.CurrentSpace, items)
	}
}

func itemTarget(item Item) string {
	switch item.Kind {
	case KindSpace:
		return item.WorkspaceID
	case KindDefinition:
		return item.DefinitionID
	case KindAgent:
		return strings.TrimPrefix(item.ID, KindAgent+":")
	default:
		return item.ID
	}
}

func previousSpaceID(history focus.History, current string, items []Item) string {
	for _, id := range history.Spaces {
		if id == current {
			continue
		}
		for _, item := range items {
			if item.Kind == KindSpace && item.WorkspaceID == id {
				return item.ID
			}
		}
	}
	return ""
}

func previousAgentID(history focus.History, current string, items []Item) string {
	for _, id := range history.Agents {
		s := id.String()
		if s == current {
			continue
		}
		for _, item := range items {
			if item.Kind == KindAgent && itemTarget(item) == s {
				return item.ID
			}
		}
	}
	return ""
}

func isCurrentItem(item Item, launch LaunchContext) bool {
	switch item.Kind {
	case KindSpace:
		return item.WorkspaceID == launch.CurrentSpace || itemTarget(item) == launch.CurrentSpace
	case KindAgent:
		return itemTarget(item) == launch.CurrentAgent
	default:
		return false
	}
}

func HasItemID(items []Item, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
