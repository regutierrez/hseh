package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// WorkspaceGit is read-only checkout detail. Empty fields mean a non-repository directory.
type WorkspaceGit struct {
	Branch string
	Status string
}

func activeWorkspaceDirectory(snapshot HerdrSessionSnapshot, workspaceID string) string {
	paneID := ActiveWorkspacePaneID(snapshot, workspaceID)
	for _, pane := range snapshot.Panes {
		if pane.PaneID == paneID {
			return firstNonEmpty(pane.ForegroundCwd, pane.Cwd)
		}
	}
	return ""
}

func readWorkspaceGit(ctx context.Context, dir string) WorkspaceGit {
	if dir == "" {
		return WorkspaceGit{}
	}
	defer traceSpan("git.status", "dir", dir)()
	cmd := exec.CommandContext(ctx, "git", "--no-optional-locks", "-C", dir, "-c", "core.fsmonitor=false", "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	output, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok && strings.Contains(string(e.Stderr), "not a git repository") {
			return WorkspaceGit{}
		}
		if ctx.Err() != nil {
			return WorkspaceGit{}
		}
		return WorkspaceGit{Status: "git status unavailable"}
	}
	return parseWorkspaceGitStatus(string(output))
}

func parseWorkspaceGitStatus(output string) WorkspaceGit {
	var result WorkspaceGit
	var oid string
	var staged, modified, untracked, conflicts, ahead, behind int
	records := strings.Split(output, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		switch {
		case strings.HasPrefix(record, "# branch.head "):
			result.Branch = strings.TrimPrefix(record, "# branch.head ")
		case strings.HasPrefix(record, "# branch.oid "):
			oid = strings.TrimPrefix(record, "# branch.oid ")
		case strings.HasPrefix(record, "# branch.ab "):
			fields := strings.Fields(record)
			if len(fields) == 4 {
				ahead, _ = strconv.Atoi(strings.TrimPrefix(fields[2], "+"))
				behind, _ = strconv.Atoi(strings.TrimPrefix(fields[3], "-"))
			}
		case strings.HasPrefix(record, "? "):
			untracked++
		case strings.HasPrefix(record, "u "):
			conflicts++
		case strings.HasPrefix(record, "1 "), strings.HasPrefix(record, "2 "):
			if len(record) >= 4 {
				if record[2] != '.' {
					staged++
				}
				if record[3] != '.' {
					modified++
				}
			}
			if record[0] == '2' {
				i++
			} // The next NUL record is the original rename path, not another status.
		}
	}
	if result.Branch == "(detached)" {
		result.Branch = "@" + oid[:min(7, len(oid))]
	}
	var status []string
	for _, count := range []struct {
		symbol string
		n      int
	}{{"+", staged}, {"~", modified}, {"?", untracked}, {"!", conflicts}, {"↑", ahead}, {"↓", behind}} {
		if count.n > 0 {
			status = append(status, fmt.Sprintf("%s%d", count.symbol, count.n))
		}
	}
	result.Branch = sanitizeTokenValue(result.Branch)
	result.Status = strings.Join(status, " ")
	return result
}

func loadWorkspaceGit(ctx context.Context, snapshot HerdrSessionSnapshot) map[string]WorkspaceGit {
	byDirectory := map[string]WorkspaceGit{}
	span := traceSpan("git.load")
	for _, workspace := range snapshot.Workspaces {
		if ctx.Err() != nil {
			break
		}
		dir := activeWorkspaceDirectory(snapshot, workspace.WorkspaceID)
		if dir == "" {
			continue
		}
		if _, exists := byDirectory[dir]; !exists {
			byDirectory[dir] = readWorkspaceGit(ctx, dir)
		}
	}
	span("dirs", len(byDirectory))
	return byDirectory
}
