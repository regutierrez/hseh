package main

import (
	"context"
	"fmt"
	"time"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/picker"
	"github.com/regutierrez/hseh/internal/space"
)

func runPluginEvent() error {
	name, data, err := herdr.PluginEventFromEnv()
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
	target, err := focus.SwitchWorkspace(snapshot, witness, time.Now().UnixMilli())
	if err != nil || target == "" {
		return err
	}
	return herdr.FocusWorkspaceContext(context.Background(), target)
}

func runPickerList(view string) error {
	doc, err := picker.LoadListDocument(context.Background(), view)
	if err != nil {
		return err
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

func runRecoverReusableSpace(definitionID, workspaceID string, create bool) error {
	result, err := space.Recover(context.Background(), definitionID, workspaceID, create)
	if err != nil {
		return err
	}
	fmt.Printf("recovered %s %s %s\n", result.Action, result.DefinitionID, result.WorkspaceID)
	return nil
}
