package picker

import (
	"strings"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
)

// launchContext names the targets focused when the popup opened, so preselection can skip them.
type launchContext struct {
	CurrentSpace string
	CurrentAgent string
}

func launchFromSnapshot(snapshot herdr.SessionSnapshot, history focus.History) launchContext {
	ctx := launchContext{CurrentSpace: snapshot.FocusedWorkspaceID}
	if snapshot.FocusedPaneID != "" {
		if id, ok := focus.CurrentAgentLiveID(history, snapshot.FocusedPaneID); ok {
			ctx.CurrentAgent = id.String()
		}
	}
	return ctx
}

// preselectItemID chooses the previous target, then first non-current, then current.
func preselectItemID(view string, items []Item, history focus.History, launch launchContext) string {
	if len(items) == 0 {
		return ""
	}
	var wanted string
	if view == ViewAgents {
		ids := make([]string, len(history.Agents))
		for i, id := range history.Agents {
			ids[i] = id.String()
		}
		wanted = previousID(ids, launch.CurrentAgent, items, KindAgent)
	} else {
		wanted = previousID(history.Spaces, launch.CurrentSpace, items, KindSpace)
	}
	if wanted != "" && hasItemID(items, wanted) {
		return wanted
	}
	for _, item := range items {
		if !isCurrentItem(item, launch) {
			return item.ID
		}
	}
	return items[0].ID
}

// itemTarget is the live agent ID behind an agent row; callers must check Kind == KindAgent.
func itemTarget(item Item) string {
	return strings.TrimPrefix(item.ID, KindAgent+":")
}

func previousID(ids []string, current string, items []Item, kind string) string {
	for _, id := range ids {
		if id == current {
			continue
		}
		for _, item := range items {
			if item.Kind != kind {
				continue
			}
			got := item.WorkspaceID
			if kind == KindAgent {
				got = itemTarget(item)
			}
			if got == id {
				return item.ID
			}
		}
	}
	return ""
}

func isCurrentItem(item Item, launch launchContext) bool {
	switch item.Kind {
	case KindSpace:
		return item.WorkspaceID == launch.CurrentSpace
	case KindAgent:
		return itemTarget(item) == launch.CurrentAgent
	default:
		return false
	}
}

func hasItemID(items []Item, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
