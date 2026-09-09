package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func validDefinitionTOML(dir, extra string) string {
	return extra + "name = \"demo\"\nworking_dir = \"" + dir + "\"\n[[tabs]]\nname = \"main\"\ncommand = \"echo MARK\"\n"
}

func TestEnsureSpaceDefinitionIDInsertsMissingTopLevel(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.toml")
	original := []byte("# keep comments\n" + validDefinitionTOML(dir, ""))
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	def, err := EnsureSpaceDefinitionID(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID == "" {
		t.Fatal("missing generated id")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(got, original) {
		t.Fatalf("original bytes not preserved:\n%s", got)
	}
	if !bytes.HasPrefix(got, []byte("id = \""+def.ID+"\"\n")) {
		t.Fatalf("id not prepended: %s", got)
	}
}

func TestEnsureSpaceDefinitionIDKeepsQuotedKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quoted.toml")
	body := validDefinitionTOML(dir, "\"id\" = \"keep-me\"\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	def, err := EnsureSpaceDefinitionID(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID != "keep-me" {
		t.Fatalf("id %q", def.ID)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, []byte(body)) {
		t.Fatalf("rewrote existing id file: %s", got)
	}
}

func TestEnsureSpaceDefinitionIDIgnoresCommentedAndTableIDs(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "nested.toml")
	body := "# id = \"commented\"\n" + validDefinitionTOML(dir, "") + "\n[meta]\nid = \"table\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	def, err := EnsureSpaceDefinitionID(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.ID == "commented" || def.ID == "table" || def.ID == "" {
		t.Fatalf("used non-top-level id %q", def.ID)
	}
}

func TestEnsureSpaceDefinitionIDRejectsBlankExistingID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blank.toml")
	if err := os.WriteFile(path, []byte(validDefinitionTOML(dir, "id = \"\"\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSpaceDefinitionID(path); err == nil {
		t.Fatal("expected empty id error")
	}
}

func TestEnsureSpaceDefinitionIDRejectsReadOnlyFile(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "ro.toml")
	if err := os.WriteFile(path, []byte(validDefinitionTOML(dir, "")), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureSpaceDefinitionID(path); err == nil {
		t.Fatal("expected read-only error")
	}
	got, _ := os.ReadFile(path)
	if bytes.Contains(got, []byte("id =")) {
		t.Fatalf("wrote id into read-only file: %s", got)
	}
}

func TestEnsureSpaceDefinitionIDConcurrentLoadsShareOneID(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "race.toml")
	if err := os.WriteFile(path, []byte(validDefinitionTOML(dir, "")), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make([]string, 2)
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			def, err := EnsureSpaceDefinitionID(path)
			errs[i] = err
			ids[i] = def.ID
		}(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errors %v %v", errs[0], errs[1])
	}
	if ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("ids %q %q", ids[0], ids[1])
	}
	got, _ := os.ReadFile(path)
	if strings.Count(string(got), "id =") != 1 {
		t.Fatalf("multiple id fields: %s", got)
	}
}

func TestLoadSpaceDefinitionsIsolatesDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "spaces")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.toml"), []byte("id = \"same\"\nname = \"alpha\"\nworking_dir = \""+work+"\"\n[[tabs]]\nname = \"main\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.toml"), []byte("id = \"same\"\nname = \"beta\"\nworking_dir = \""+work+"\"\n[[tabs]]\nname = \"main\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.toml"), []byte("id = \"other\"\nname = \"gamma\"\nworking_dir = \""+work+"\"\n[[tabs]]\nname = \"main\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defs, errs := LoadSpaceDefinitions(dir)
	if len(defs) != 1 || defs[0].ID != "other" {
		t.Fatalf("defs %+v errs %v", defs, errs)
	}
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), "duplicate id same") {
		t.Fatalf("missing duplicate error: %v", errs)
	}
}

func TestOmittedRatioIsEvenSplit(t *testing.T) {
	pane := SpaceDefinitionPane{}
	if pane.splitRatio() != 0 {
		t.Fatalf("omitted ratio %v", pane.splitRatio())
	}
	pane.Ratio = 0.25
	if pane.splitRatio() != 0.75 {
		t.Fatalf("authored share 0.25 should become 0.75, got %v", pane.splitRatio())
	}
}

func TestPromptWorkingDirRejected(t *testing.T) {
	def := SpaceDefinition{Name: "x", WorkingDir: "{prompt}", Tabs: []SpaceDefinitionTab{{Name: "t"}}, SourceFile: "x.toml"}
	if err := validateSpaceDefinition(def); err == nil {
		t.Fatal("expected prompt rejection")
	}
}

func TestReplaceSpaceDefinitionFileDetectsConcurrentEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "race.toml")
	original := []byte(validDefinitionTOML(dir, ""))
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	edited := append([]byte("# edited\n"), original...)
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	next := append([]byte("id = \"new\"\n"), original...)
	err := replaceSpaceDefinitionFile(path, original, next)
	if err == nil || !strings.Contains(err.Error(), "changed while inserting id") {
		t.Fatalf("expected change detection, got %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, edited) {
		t.Fatalf("overwrote concurrent edit: %s", got)
	}
}

func TestCanonicalSpaceDefinitionPathUnifiesRelativeAndAbsolute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.toml")
	if err := os.WriteFile(path, []byte("name = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	abs := canonicalSpaceDefinitionPath(path)
	rel := canonicalSpaceDefinitionPath("demo.toml")
	if abs != rel {
		t.Fatalf("relative and absolute aliases differ: %q vs %q", rel, abs)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("canonical path is not absolute: %q", abs)
	}
}

func TestEnsureSpaceDefinitionIDWritesThroughSymlink(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "real.toml")
	original := []byte(validDefinitionTOML(dir, ""))
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	def, err := EnsureSpaceDefinitionID(link)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced: %v", info)
	}
	got, _ := os.ReadFile(target)
	if !bytes.HasSuffix(got, original) || !bytes.Contains(got, []byte(def.ID)) {
		t.Fatalf("target not updated in place: %s", got)
	}
}
