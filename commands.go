package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/picker"
	"github.com/regutierrez/hseh/internal/space"
)

func runPluginEvent() error {
	name := os.Getenv("HERDR_PLUGIN_EVENT")
	if name == "" {
		return fmt.Errorf("hseh event: HERDR_PLUGIN_EVENT is not set")
	}
	raw := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	if raw == "" {
		return fmt.Errorf("hseh event: HERDR_PLUGIN_EVENT_JSON is missing")
	}
	_, data, err := herdr.ParsePluginEventJSON(raw)
	if err != nil {
		return err
	}
	return focus.ApplyPluginEvent(name, data)
}

func runWorkspaceSwitch() error {
	snapshot, witness, err := herdr.LoadSessionSnapshot()
	if err != nil {
		return err
	}
	stateDir := config.StateDir()
	var target string
	if err := focus.WithLock(stateDir, func() error {
		history, loadErr := focus.LoadValidated(stateDir, witness)
		if loadErr != nil {
			return loadErr
		}
		history, target = focus.SelectNextWorkspace(history, snapshot, time.Now().UnixMilli())
		return focus.WriteFile(stateDir, history)
	}); err != nil {
		return err
	}
	if target == "" {
		return nil
	}
	return herdr.FocusWorkspace(target)
}

func runPickerList(view string) error {
	view, err := picker.ParseView(view)
	if err != nil {
		return err
	}
	snapshot, witness, err := herdr.LoadSessionSnapshot()
	if err != nil {
		return err
	}
	history, err := focus.LoadPruned(snapshot, witness)
	if err != nil {
		return err
	}
	layout, configErrs := picker.LoadSidebarLayout("")
	_, moreErrs := config.LoadPreviewPollInterval()
	configErrs = append(configErrs, moreErrs...)
	definitions, defErrs := space.LoadDefinitions(config.SpaceDefinitionsDir())
	configErrs = append(configErrs, defErrs...)
	state, assocErr := space.LoadReconciledAssociationState(config.StateDir(), witness)
	var records, unresolved []space.AssociationRecord
	if assocErr != nil {
		configErrs = append(configErrs, assocErr.Error())
	} else {
		records = state.Records
		unresolved = state.Unresolved
	}
	if view != picker.ViewAgents {
		snapshot.GitByDirectory = picker.LoadWorkspaceGit(context.Background(), snapshot)
	}
	items := picker.AppendUnopenedDefinitionItems(picker.BuildItemsWithLayout(view, snapshot, history, layout), view, snapshot, definitions, records, unresolved)
	doc := picker.ListDocument{
		Session: picker.ListSession{Name: herdr.SessionName(config.SocketPath()), SocketPath: config.SocketPath()},
		View:    view,
		Items:   items,
		Errors:  configErrs,
	}
	payload, err := picker.EncodeListJSON(doc)
	if err != nil {
		return err
	}
	fmt.Println(string(payload))
	return nil
}

func runLaunch(view string) error {
	view, err := picker.ParseView(view)
	if err != nil {
		return err
	}
	return herdr.OpenPluginPopup(view)
}

func runOpenReusableSpace(definitionID string) error {
	result, err := space.Open(context.Background(), definitionID)
	if err != nil {
		return err
	}
	fmt.Printf("opened %s %s %s\n", result.Action, result.DefinitionID, result.WorkspaceID)
	return nil
}

func parseRecoverArgs(args []string) (definitionID, workspaceID string, create bool, err error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" || strings.HasPrefix(args[0], "-") {
		return "", "", false, fmt.Errorf("hseh recover: definition id is required")
	}
	definitionID = strings.TrimSpace(args[0])
	seenCreate := false
	seenWorkspace := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--create":
			if seenCreate {
				return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
			}
			seenCreate = true
			create = true
		case arg == "--workspace":
			if seenWorkspace {
				return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
			}
			seenWorkspace = true
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("hseh recover: --workspace requires a live workspace id")
			}
			i++
			workspaceID = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--workspace="):
			if seenWorkspace {
				return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
			}
			seenWorkspace = true
			workspaceID = strings.TrimSpace(arg[len("--workspace="):])
		default:
			return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
		}
	}
	if seenWorkspace && workspaceID == "" {
		return "", "", false, fmt.Errorf("hseh recover: --workspace requires a live workspace id")
	}
	if seenCreate == seenWorkspace {
		return "", "", false, fmt.Errorf("hseh recover: exactly one of --workspace <live-workspace-id> or --create is required")
	}
	return definitionID, workspaceID, create, nil
}

func runRecoverReusableSpace(args []string) error {
	definitionID, workspaceID, create, err := parseRecoverArgs(args)
	if err != nil {
		return err
	}
	result, err := space.Recover(context.Background(), definitionID, workspaceID, create)
	if err != nil {
		return err
	}
	fmt.Printf("recovered %s %s %s\n", result.Action, result.DefinitionID, result.WorkspaceID)
	return nil
}
