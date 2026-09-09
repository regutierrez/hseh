package space

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/lockfile"
	"github.com/regutierrez/hseh/internal/termtext"
)

const (
	promptDir      = "{prompt}"
	maxPanesPerTab = 4
	splitDown      = "down"
	splitRight     = "right"
)

// Definition is one fixed-directory reusable space from a TOML file.
type Definition struct {
	ID          string          `toml:"id"`
	Name        string          `toml:"name"`
	Description string          `toml:"description"`
	WorkingDir  string          `toml:"working_dir"`
	Tabs        []DefinitionTab `toml:"tabs"`
	SourceFile  string          `toml:"-"`
	ResolvedDir string          `toml:"-"`
}

// DefinitionTab is one tab in a reusable space definition.
type DefinitionTab struct {
	Name       string           `toml:"name"`
	Command    string           `toml:"command"`
	WorkingDir string           `toml:"working_dir"`
	Panes      []DefinitionPane `toml:"panes"`
}

// DefinitionPane is one pane in a tab. Split and Ratio apply after the root pane.
type DefinitionPane struct {
	Command    string  `toml:"command"`
	Split      string  `toml:"split"`
	Label      string  `toml:"label"`
	Ratio      float64 `toml:"ratio"`
	WorkingDir string  `toml:"working_dir"`
}

func newDefinitionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("hseh definition: generate id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func decodeDefinitionTOML(payload []byte) (Definition, toml.MetaData, error) {
	var def Definition
	meta, err := toml.Decode(string(payload), &def)
	if err != nil {
		return Definition{}, meta, err
	}
	return def, meta, nil
}

func definitionHasTopLevelID(meta toml.MetaData) bool {
	return meta.IsDefined("id")
}

func validateDefinitionIDValue(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("id is empty")
	}
	if trimmed != id {
		return fmt.Errorf("id has surrounding whitespace")
	}
	for _, r := range id {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return fmt.Errorf("id contains whitespace or non-printable characters")
		}
	}
	return nil
}

func validateDefinition(def Definition) error {
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("hseh definition %s: name is required", def.SourceFile)
	}
	workingDir := strings.TrimSpace(def.WorkingDir)
	if workingDir == "" {
		return fmt.Errorf("hseh definition %q (%s): working_dir is required", def.Name, def.SourceFile)
	}
	if workingDir == promptDir {
		return fmt.Errorf("hseh definition %q (%s): working_dir {prompt} is not supported", def.Name, def.SourceFile)
	}
	if len(def.Tabs) == 0 {
		return fmt.Errorf("hseh definition %q (%s): needs at least one [[tabs]] entry", def.Name, def.SourceFile)
	}
	return validateDefinitionTabs(def.Name, def.SourceFile, def.Tabs)
}

// validateDefinitionTabs copies Herdr Plus tab rules: name required, command or panes, at most 4 panes,
// split down/right, omitted ratio is an even split. See third_party/herdr-plus/NOTICE.
func validateDefinitionTabs(label, source string, tabs []DefinitionTab) error {
	for i, tab := range tabs {
		if strings.TrimSpace(tab.Name) == "" {
			return fmt.Errorf("%q (%s): tab %d is missing a name", label, source, i+1)
		}
		if len(tab.Panes) > 0 && strings.TrimSpace(tab.Command) != "" {
			return fmt.Errorf("%q (%s): tab %q sets both command and [[tabs.panes]]; use one or the other", label, source, tab.Name)
		}
		if len(tab.Panes) > maxPanesPerTab {
			return fmt.Errorf("%q (%s): tab %q has %d panes; at most %d are allowed", label, source, tab.Name, len(tab.Panes), maxPanesPerTab)
		}
		for j, pane := range tab.Panes {
			if j == 0 {
				continue
			}
			switch pane.Split {
			case "", splitDown, splitRight:
			default:
				return fmt.Errorf("%q (%s): tab %q pane %d has split %q; must be %q or %q", label, source, tab.Name, j+1, pane.Split, splitDown, splitRight)
			}
			if math.IsNaN(pane.Ratio) || math.IsInf(pane.Ratio, 0) || pane.Ratio < 0 || pane.Ratio >= 1 {
				return fmt.Errorf("%q (%s): tab %q pane %d has ratio %v; must be greater than 0 and less than 1", label, source, tab.Name, j+1, pane.Ratio)
			}
		}
	}
	return nil
}

func loadDefinitionFromBytes(path string, payload []byte) (Definition, toml.MetaData, error) {
	def, meta, err := decodeDefinitionTOML(payload)
	if err != nil {
		return Definition{}, meta, fmt.Errorf("hseh definition: parse %s: %w", filepath.Base(path), err)
	}
	def.SourceFile = filepath.Base(path)
	return def, meta, nil
}

func canonicalDefinitionPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	canonical, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	return canonical
}

func definitionIDLockPath(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(config.StateDir(), "definition-id-locks", hex.EncodeToString(sum[:])+".lock")
}

func definitionIDPrefix(id string, original []byte) []byte {
	nl := "\n"
	if bytes.Contains(original, []byte("\r\n")) {
		nl = "\r\n"
	}
	return []byte("id = \"" + id + "\"" + nl)
}

func definitionFileWritable(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("hseh definition: %s is not writable: %w", path, err)
	}
	return file.Close()
}

func definitionReplaceDest(path string) (dest string, mode os.FileMode, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, fmt.Errorf("hseh definition: stat %s: %w", path, err)
	}
	dest = path
	mode = info.Mode().Perm()
	if info.Mode()&os.ModeSymlink != 0 {
		dest, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", 0, fmt.Errorf("hseh definition: resolve %s: %w", path, err)
		}
		destInfo, err := os.Stat(dest)
		if err != nil {
			return "", 0, fmt.Errorf("hseh definition: stat %s: %w", dest, err)
		}
		mode = destInfo.Mode().Perm()
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", 0, err
	}
	return abs, mode, nil
}

// replaceDefinitionFile writes next via a same-directory temp file and rename onto the
// regular-file destination (symlink target, never the symlink). A concurrent editor write or
// destination change observed on re-check fails instead of overwriting. Between that re-check
// and rename an OS race remains; advisory hseh locks do not serialize external editors.
func replaceDefinitionFile(path string, original, next []byte) error {
	dest, mode, err := definitionReplaceDest(path)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(dest), ".hseh-id-*")
	if err != nil {
		return fmt.Errorf("hseh definition: temp %s: %w", dest, err)
	}
	tempName := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("hseh definition: chmod %s: %w", tempName, err)
	}
	if _, err := temp.Write(next); err != nil {
		_ = temp.Close()
		return fmt.Errorf("hseh definition: write %s: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("hseh definition: close %s: %w", tempName, err)
	}
	latest, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("hseh definition: re-read %s: %w", path, err)
	}
	if !bytes.Equal(latest, original) {
		return fmt.Errorf("hseh definition: %s changed while inserting id", filepath.Base(path))
	}
	laterDest, _, err := definitionReplaceDest(path)
	if err != nil {
		return err
	}
	if laterDest != dest {
		return fmt.Errorf("hseh definition: %s destination changed while inserting id", filepath.Base(path))
	}
	if err := os.Rename(tempName, dest); err != nil {
		return fmt.Errorf("hseh definition: replace %s: %w", dest, err)
	}
	removeTemp = false
	return nil
}

func parseExistingDefinition(path string, payload []byte) (Definition, toml.MetaData, error) {
	def, meta, err := loadDefinitionFromBytes(path, payload)
	if err != nil {
		return Definition{}, meta, err
	}
	if definitionHasTopLevelID(meta) {
		if err := validateDefinitionIDValue(def.ID); err != nil {
			return Definition{}, meta, fmt.Errorf("hseh definition %s: %w", def.SourceFile, err)
		}
	}
	if err := validateDefinition(def); err != nil {
		return Definition{}, meta, err
	}
	return def, meta, nil
}

func insertMissingDefinitionID(path string) (Definition, error) {
	if err := definitionFileWritable(path); err != nil {
		return Definition{}, err
	}
	canonical := canonicalDefinitionPath(path)
	var def Definition
	err := lockfile.WithExclusive(definitionIDLockPath(canonical), func() error {
		current, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("hseh definition: read %s: %w", path, err)
		}
		parsed, meta, err := parseExistingDefinition(path, current)
		if err != nil {
			return err
		}
		if definitionHasTopLevelID(meta) {
			def = parsed
			return nil
		}
		id, err := newDefinitionID()
		if err != nil {
			return err
		}
		inserted := append(definitionIDPrefix(id, current), current...)
		if err := replaceDefinitionFile(path, current, inserted); err != nil {
			return err
		}
		parsed.ID = id
		def = parsed
		return nil
	})
	return def, err
}

// EnsureDefinitionID loads a definition. Existing top-level ids are a read-only
// fast path with no adjacent lock file. Missing ids take a state-scoped lock.
func EnsureDefinitionID(path string) (Definition, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, fmt.Errorf("hseh definition: read %s: %w", path, err)
	}
	def, meta, err := parseExistingDefinition(path, payload)
	if err != nil {
		return Definition{}, err
	}
	if definitionHasTopLevelID(meta) {
		return def, nil
	}
	return insertMissingDefinitionID(path)
}

func resolveDefinitionDir(def Definition) (string, error) {
	expanded, err := expandPath(def.WorkingDir)
	if err != nil {
		return "", fmt.Errorf("hseh definition %q (%s): working_dir: %w", def.Name, def.SourceFile, err)
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("hseh definition %q (%s): working_dir: %w", def.Name, def.SourceFile, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return filepath.Clean(abs), nil
}

// LoadDefinitions reads valid *.toml files from the plugin spaces directory.
func LoadDefinitions(dir string) ([]Definition, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("hseh definition: read %s: %v", dir, err)}
	}
	type loaded struct {
		def  Definition
		file string
	}
	var loadedDefs []loaded
	var errs []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		def, err := EnsureDefinitionID(path)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		resolved, err := resolveDefinitionDir(def)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		def.ResolvedDir = resolved
		loadedDefs = append(loadedDefs, loaded{def: def, file: entry.Name()})
	}
	byID := map[string][]loaded{}
	for _, item := range loadedDefs {
		byID[item.def.ID] = append(byID[item.def.ID], item)
	}
	var defs []Definition
	for id, items := range byID {
		if len(items) > 1 {
			var files []string
			for _, item := range items {
				files = append(files, item.file)
			}
			errs = append(errs, fmt.Sprintf("hseh definition: duplicate id %s in %s", id, strings.Join(files, ", ")))
			continue
		}
		defs = append(defs, items[0].def)
	}
	return defs, errs
}

func FindDefinitionByID(defs []Definition, id string) (Definition, bool) {
	for _, def := range defs {
		if def.ID == id {
			return def, true
		}
	}
	return Definition{}, false
}

func SanitizeDisplayText(value string) string {
	return strings.TrimSpace(termtext.StripControls(value))
}

func DefinitionPreviewText(def Definition) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s\n%s\n", SanitizeDisplayText(def.Name), SanitizeDisplayText(def.ResolvedDir))
	if desc := SanitizeDisplayText(def.Description); desc != "" {
		fmt.Fprintf(&b, "%s\n", desc)
	}
	for _, tab := range def.Tabs {
		fmt.Fprintf(&b, "tab %s\n", SanitizeDisplayText(tab.Name))
		for _, pane := range tab.effectivePanes() {
			cmd := SanitizeDisplayText(pane.Command)
			if cmd == "" {
				cmd = "(shell)"
			}
			fmt.Fprintf(&b, "  %s\n", cmd)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
