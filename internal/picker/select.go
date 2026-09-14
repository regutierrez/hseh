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
// Agents prefer a done row over most recently used.
func preselectItemID(view string, items []Item, history focus.History, launch launchContext) string {
	if len(items) == 0 {
		return ""
	}
	var wanted string
	if view == ViewAgents {
		wanted = previousAgentID(history, launch.CurrentAgent, items)
	} else {
		wanted = previousSpaceID(history, launch.CurrentSpace, items)
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
	if id := firstDoneAgentID(items, current); id != "" {
		return id
	}
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

// firstDoneAgentID returns the first agent tagged as done, skipping the focused pane.
func firstDoneAgentID(items []Item, current string) string {
	for _, item := range items {
		if item.Kind != KindAgent || item.Status != "done" {
			continue
		}
		if itemTarget(item) == current {
			continue
		}
		return item.ID
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
