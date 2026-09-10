package gitinfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestParseStatusCountsRenameConflictAndDivergence(t *testing.T) {
	payload := "# branch.oid abcdef123456\x00# branch.head topic\x00# branch.ab +2 -3\x00" +
		"2 R. N... 100644 100644 100644 abc abc R100 renamed\x00? original-name\x00" +
		"? untracked\x00u UU N... conflict\x00"
	got := parseStatus(payload)
	if got.Branch != "topic" || got.Status != "+1 ?1 !1 ↑2 ↓3" {
		t.Fatalf("%+v", got)
	}
	detached := parseStatus("# branch.oid abcdef123456\x00# branch.head (detached)\x00")
	if detached.Branch != "@abcdef1" || detached.Status != "" {
		t.Fatalf("%+v", detached)
	}
}

func TestReadRealCheckoutWithoutMutatingIt(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hsehtest.Git(t, dir, "init", "-b", "main")
	write("tracked", "one")
	hsehtest.Git(t, dir, "add", "tracked")
	hsehtest.Git(t, dir, "commit", "-m", "initial")
	write("tracked", "two")
	write("staged", "new")
	hsehtest.Git(t, dir, "add", "staged")
	write("untracked", "new")
	before := hsehtest.Git(t, dir, "status", "--porcelain")
	got := Read(context.Background(), dir)
	if got.Branch != "main" || got.Status != "+1 ~1 ?1" || got.Root != dir {
		t.Fatalf("git details %+v", got)
	}
	if after := hsehtest.Git(t, dir, "status", "--porcelain"); after != before {
		t.Fatal("git status mutated checkout")
	}
	if got := Read(context.Background(), t.TempDir()); got != (WorkspaceGit{}) {
		t.Fatalf("nonrepo %+v", got)
	}
}
