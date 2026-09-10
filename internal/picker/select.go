package picker

import (
	"strings"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
)

// LaunchContext names the targets focused when the popup opened, so preselection can skip them.
type LaunchContext struct {
	CurrentSpace string
	CurrentAgent string
}

func launchFromSnapshot(snapshot herdr.SessionSnapshot, history focus.History) LaunchContext {
	ctx := LaunchContext{CurrentSpace: snapshot.FocusedWorkspaceID}
	if snapshot.FocusedPaneID != "" {
		if id, ok := focus.CurrentAgentLiveID(history, snapshot.FocusedPaneID); ok {
			ctx.CurrentAgent = id.String()
		}
	}
	return ctx
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
		return item.WorkspaceID == launch.CurrentSpace
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
