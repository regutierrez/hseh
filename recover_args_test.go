package main

import "testing"

func TestOracleRecoverRejectsEmptyWorkspaceWithCreate(t *testing.T) {
	for _, args := range [][]string{{"demo", "--workspace=", "--create"}, {"demo", "--workspace", "", "--create"}} {
		_, _, _, err := parseRecoverArgs(args)
		if err == nil {
			t.Errorf("ambiguous recovery can create workspace: %q", args)
		}
	}
}
