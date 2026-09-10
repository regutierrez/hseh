package picker

import (
	"context"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
)

// liveState is one session.snapshot round trip plus the history and associations
// that depend on its continuity witness. assocErr is set when the association file
// is unreadable; the snapshot and history are still valid then.
type liveState struct {
	snapshot   herdr.SessionSnapshot
	history    focus.History
	records    []space.AssociationRecord
	unresolved []space.AssociationRecord
	assocErr   error
}

func loadLive(ctx context.Context) (liveState, error) {
	snapshot, witness, err := herdr.LoadSessionSnapshotContext(ctx)
	if err != nil {
		return liveState{}, err
	}
	history, err := focus.LoadPruned(snapshot, witness)
	if err != nil {
		return liveState{}, err
	}
	live := liveState{snapshot: snapshot, history: history}
	state, err := space.LoadReconciledAssociationState(config.StateDir(), witness)
	if err != nil {
		live.assocErr = err
		return live, nil
	}
	live.records, live.unresolved = state.Records, state.Unresolved
	return live, nil
}

func loadCatalog() (sidebarLayout, []space.Definition, []string) {
	layout, errs := loadSidebarLayout("")
	definitions, defErrs := space.LoadDefinitions(config.SpaceDefinitionsDir())
	return layout, definitions, append(errs, defErrs...)
}

// assembleItems is the one place rows are built: live rows first, then unopened templates.
func assembleItems(view string, live liveState, layout sidebarLayout, definitions []space.Definition, git map[string]gitinfo.WorkspaceGit) []Item {
	items := buildItemsWithLayout(view, live.snapshot, live.history, layout, git)
	return appendUnopenedDefinitionItems(items, view, live.snapshot, definitions, live.records, live.unresolved, git)
}

// LoadListDocument assembles the rows the popup would show, for `hseh list --json`.
func LoadListDocument(ctx context.Context, view string) (ListDocument, error) {
	view, err := ParseView(view)
	if err != nil {
		return ListDocument{}, err
	}
	_, errs := config.Load()
	layout, definitions, catalogErrs := loadCatalog()
	errs = append(errs, catalogErrs...)
	live, err := loadLive(ctx)
	if err != nil {
		return ListDocument{}, err
	}
	if live.assocErr != nil {
		errs = append(errs, live.assocErr.Error())
	}
	var git map[string]gitinfo.WorkspaceGit
	if view != ViewAgents {
		git = loadGit(ctx, live.snapshot, definitions)
	}
	return ListDocument{
		Session: ListSession{Name: config.SessionName(), SocketPath: config.SocketPath()},
		View:    view,
		Items:   assembleItems(view, live, layout, definitions, git),
		Errors:  errs,
	}, nil
}
