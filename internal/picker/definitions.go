package picker

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
)

func SelectionID(kind, target string) string {
	return kind + ":" + target
}

func definitionItem(def space.Definition, needsRecovery bool) Item {
	name := space.SanitizeDisplayText(def.Name)
	desc := space.SanitizeDisplayText(def.Description)
	dir := space.SanitizeDisplayText(def.ResolvedDir)
	if dir == "" {
		dir = space.SanitizeDisplayText(def.WorkingDir)
	}
	rows := []string{name}
	if desc != "" {
		rows = append(rows, desc)
	}
	if dir != "" {
		rows = append(rows, dir)
	}
	preview := space.DefinitionPreviewText(def)
	if needsRecovery {
		hints := space.RecoveryHintLines(def.ID)
		rows = append(rows, hints...)
		preview = strings.Join(hints, "\n") + "\n" + preview
	}
	search := strings.Join(append(append([]string{}, rows...), space.SanitizeDisplayText(def.SourceFile), space.SanitizeDisplayText(filepath.Base(def.SourceFile))), " ")
	display := make([]string, len(rows))
	for i, row := range rows {
		if i == 0 {
			display[i] = "\x1b[1m" + row + "\x1b[0m"
		} else {
			display[i] = mutedSGR + row + "\x1b[0m"
		}
	}
	return Item{
		Kind:         KindDefinition,
		ID:           SelectionID(KindDefinition, def.ID),
		DefinitionID: def.ID,
		Label:        name,
		Rows:         rows,
		DisplayRows:  display,
		SearchText:   search,
		PreviewText:  preview,
	}
}

func AppendUnopenedDefinitionItems(items []Item, view string, snapshot herdr.SessionSnapshot, definitions []space.Definition, records, unresolved []space.AssociationRecord) []Item {
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
		extra = append(extra, definitionItem(def, unresolvedKeys[space.AssociationIdentityKey(def.ID, def.ResolvedDir)]))
	}
	sort.SliceStable(extra, func(i, j int) bool { return extra[i].Label < extra[j].Label })
	return append(items, extra...)
}
