package picker

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
)

func selectionID(kind, target string) string {
	return kind + ":" + target
}

func definitionItem(def space.Definition, needsRecovery bool, git gitinfo.WorkspaceGit) Item {
	name := space.SanitizeDisplayText(def.Name)
	desc := space.SanitizeDisplayText(def.Description)
	dir := space.SanitizeDisplayText(def.ResolvedDir)
	if dir == "" {
		dir = space.SanitizeDisplayText(def.WorkingDir)
	}
	row := spaceRow{source: SourceTemplate, name: name, git: git, path: dir}
	var recovery []string
	preview := ""
	if needsRecovery {
		hints := space.RecoveryHintLines(def.ID)
		row.tag = hints[0]
		recovery = hints[1:]
		preview = strings.Join(hints, "\n")
	}
	plain, display := renderSpaceRow(row, "") // templates carry no agent status
	search := strings.Join([]string{plain, desc, space.SanitizeDisplayText(def.SourceFile), space.SanitizeDisplayText(filepath.Base(def.SourceFile))}, " ")
	return Item{
		Kind:         KindDefinition,
		ID:           selectionID(KindDefinition, def.ID),
		DefinitionID: def.ID,
		Label:        name,
		Source:       SourceTemplate,
		Path:         dir,
		Recovery:     recovery,
		Rows:         []string{plain},
		DisplayRows:  []string{display},
		SearchText:   search,
		PreviewText:  preview,
	}
}

// appendUnopenedDefinitionItems adds a template row for every definition without a live
// associated workspace, sorted by name after the live rows.
func appendUnopenedDefinitionItems(items []Item, view string, snapshot herdr.SessionSnapshot, definitions []space.Definition, records, unresolved []space.AssociationRecord, git map[string]gitinfo.WorkspaceGit) []Item {
	if view == ViewAgents {
		return items
	}
	openKeys := space.AssociatedLiveDefinitionKeys(space.AssociationState{Records: records}, snapshot)
	unresolvedKeys := map[string]bool{}
	for _, record := range unresolved {
		unresolvedKeys[space.AssociationIdentityKey(record.DefinitionID, record.ResolvedDir)] = true
	}
	var extra []Item
	for _, def := range definitions {
		if openKeys[space.AssociationIdentityKey(def.ID, def.ResolvedDir)] {
			continue
		}
		extra = append(extra, definitionItem(def, unresolvedKeys[space.AssociationIdentityKey(def.ID, def.ResolvedDir)], git[def.ResolvedDir]))
	}
	sort.SliceStable(extra, func(i, j int) bool { return extra[i].Label < extra[j].Label })
	return append(items, extra...)
}
