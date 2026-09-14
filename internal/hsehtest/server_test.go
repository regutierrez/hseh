package hsehtest

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
)

func TestUnknownMethodFails(t *testing.T) {
	server := &Server{}
	socket, _ := Start(t, server)
	code, result := rawCall(t, socket, "no.such.method", map[string]any{})
	if code != "unknown_method" {
		t.Fatalf("unknown method code %q result %s, want unknown_method", code, result)
	}
}

func TestTabCreateAndPaneSplitAllocateUniqueIDsAndMutateSnapshot(t *testing.T) {
	server := &Server{}
	socket, _ := Start(t, server)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	ctx := context.Background()

	ws, rootTab, rootPane, err := herdr.CreateWorkspace(ctx, "/tmp/work", "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	tab1, pane1, err := herdr.CreateTab(ctx, ws, "one", "/tmp/one", false)
	if err != nil {
		t.Fatal(err)
	}
	tab2, pane2, err := herdr.CreateTab(ctx, ws, "two", "/tmp/two", false)
	if err != nil {
		t.Fatal(err)
	}
	split1, err := herdr.SplitPane(ctx, rootPane, "right", 0, "/tmp/split1", false)
	if err != nil {
		t.Fatal(err)
	}
	split2, err := herdr.SplitPane(ctx, split1, "down", 0, "/tmp/split2", false)
	if err != nil {
		t.Fatal(err)
	}

	ids := []string{rootTab, tab1, tab2, rootPane, pane1, pane2, split1, split2}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			t.Fatalf("ids not unique: %v", ids)
		}
		seen[id] = true
	}

	snap, _, err := herdr.LoadSessionSnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTab(snap, tab1, "one") || !hasTab(snap, tab2, "two") {
		t.Fatalf("tabs missing from snapshot: %+v", snap.Tabs)
	}
	if !hasPane(snap, pane1, tab1, "/tmp/one") || !hasPane(snap, split2, rootTab, "/tmp/split2") {
		t.Fatalf("panes missing from snapshot: %+v", snap.Panes)
	}
}

func TestTabAndPaneRenameAndCloseMutateSnapshot(t *testing.T) {
	server := &Server{}
	socket, _ := Start(t, server)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	ctx := context.Background()

	ws, rootTab, rootPane, err := herdr.CreateWorkspace(ctx, "/tmp/work", "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	extraTab, extraPane, err := herdr.CreateTab(ctx, ws, "extra", "/tmp/extra", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := herdr.RenameTab(ctx, extraTab, "renamed-tab"); err != nil {
		t.Fatal(err)
	}
	if err := herdr.RenamePane(ctx, extraPane, "renamed-pane"); err != nil {
		t.Fatal(err)
	}
	snap, _, err := herdr.LoadSessionSnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTab(snap, extraTab, "renamed-tab") || !hasPaneLabel(snap, extraPane, "renamed-pane") {
		t.Fatalf("rename did not mutate snapshot: tabs=%+v panes=%+v", snap.Tabs, snap.Panes)
	}

	if err := herdr.CloseTab(ctx, extraTab); err != nil {
		t.Fatal(err)
	}
	snap, _, err = herdr.LoadSessionSnapshotContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hasTab(snap, extraTab, "renamed-tab") || hasPane(snap, extraPane, extraTab, "/tmp/extra") {
		t.Fatalf("close left tab/pane in snapshot: tabs=%+v panes=%+v", snap.Tabs, snap.Panes)
	}
	if !hasTab(snap, rootTab, "demo") || !hasPane(snap, rootPane, rootTab, "/tmp/work") {
		t.Fatalf("close removed the wrong tab: tabs=%+v panes=%+v", snap.Tabs, snap.Panes)
	}
}

func rawCall(t *testing.T, socket, method string, params any) (errCode string, result json.RawMessage) {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload, err := json.Marshal(map[string]any{"id": "t1", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != nil {
		return env.Error.Code, env.Result
	}
	return "", env.Result
}

func hasTab(snap herdr.SessionSnapshot, id, label string) bool {
	for _, tab := range snap.Tabs {
		if tab.TabID == id && tab.Label == label {
			return true
		}
	}
	return false
}

func hasPane(snap herdr.SessionSnapshot, id, tabID, cwd string) bool {
	for _, pane := range snap.Panes {
		if pane.PaneID == id && pane.TabID == tabID && pane.Cwd == cwd {
			return true
		}
	}
	return false
}

func hasPaneLabel(snap herdr.SessionSnapshot, id, label string) bool {
	for _, pane := range snap.Panes {
		if pane.PaneID == id && pane.Label == label {
			return true
		}
	}
	return false
}
