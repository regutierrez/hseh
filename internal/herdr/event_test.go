package herdr

import "testing"

func TestParsePluginEventJSONPreservesLifecycleData(t *testing.T) {
	data, err := parsePluginEventJSON(`{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"pi","released":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	if data.Agent != "pi" || !data.Released || PluginEventPaneID(data) != "w1:p1" {
		t.Fatalf("real envelope loses lifecycle data: %+v", data)
	}
	nested, err := parsePluginEventJSON(`{"event":"pane_moved","data":{"previous_pane_id":"w1:p1","pane":{"pane_id":"w2:p9"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if PluginEventPaneID(nested) != "w2:p9" || nested.PreviousPaneID != "w1:p1" {
		t.Fatalf("nested pane id lost: %+v", nested)
	}
}

func TestSameContinuityWitnessRequiresPidAndStartTime(t *testing.T) {
	stored := ContinuityWitness{SocketPath: "/tmp/a", PeerPID: 1, PeerStartTime: "1"}
	if !SameContinuityWitness(stored, stored) {
		t.Fatal("identical witness must match")
	}
	if SameContinuityWitness(ContinuityWitness{SocketPath: "/tmp/a"}, ContinuityWitness{SocketPath: "/tmp/a"}) {
		t.Fatal("socket path alone must not prove continuity")
	}
	if SameContinuityWitness(stored, ContinuityWitness{SocketPath: "/tmp/a", PeerPID: 1, PeerStartTime: "2"}) {
		t.Fatal("restarted peer must not match")
	}
}
