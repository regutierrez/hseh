package space

import (
	"os"
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
)

func TestReconcileMovesCurrentRecordsToUnresolvedWithoutWrite(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HERDR_SESSION", "hseh-test")
	stale := AssociationState{
		Witness: herdr.ContinuityWitness{SocketPath: "/tmp/s", PeerPID: 9, PeerStartTime: "1", BootTime: "1"},
		Records: []AssociationRecord{
			{DefinitionID: "def-a", ResolvedDir: "/tmp/a", WorkspaceID: "w1"},
			{DefinitionID: "def-b", ResolvedDir: "/tmp/b", WorkspaceID: "w2"},
		},
	}
	if err := WriteAssociationFile(stateDir, stale); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(associationStatePath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	live := herdr.ContinuityWitness{SocketPath: "/tmp/s", PeerPID: 9, PeerStartTime: "2", BootTime: "1"}
	got, err := LoadReconciledAssociationState(stateDir, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 0 {
		t.Fatalf("current records after mismatch: %+v", got.Records)
	}
	if len(got.Unresolved) != 2 {
		t.Fatalf("unresolved %+v", got.Unresolved)
	}
	after, err := os.ReadFile(associationStatePath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("picker-style load wrote disk")
	}
}
