package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/regutierrez/hseh/internal/picker"
	"github.com/regutierrez/hseh/internal/trace"
)

func main() {
	if trace.Enabled() {
		start := []any{"args", strings.Join(os.Args[1:], " "), "unix_ms", time.Now().UnixMilli()}
		if age, ok := trace.ProcessAge(); ok {
			start = append(start, "since_exec_ms", age.Milliseconds())
		}
		trace.Event("process.start", start...)
	}
	if err := runHseh(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func viewArg(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return picker.ViewSpaces
}

func runHseh(args []string) error {
	if len(args) == 0 {
		return picker.Run(picker.ViewSpaces)
	}
	switch args[0] {
	case "popup":
		return picker.Run(viewArg(args))
	case "launch":
		return runLaunch(viewArg(args))
	case "list":
		view, err := parseListArgs(args[1:])
		if err != nil {
			return err
		}
		return runPickerList(view)
	case "switch":
		return runWorkspaceSwitch()
	case "event":
		return runPluginEvent()
	case "open":
		if len(args) < 2 {
			return fmt.Errorf("hseh open: definition id is required")
		}
		return runOpenReusableSpace(args[1])
	case "recover":
		definitionID, workspaceID, create, err := parseRecoverArgs(args[1:])
		if err != nil {
			return err
		}
		return runRecoverReusableSpace(definitionID, workspaceID, create)
	default:
		return fmt.Errorf("hseh: unknown command %s", args[0])
	}
}

func parseListArgs(args []string) (view string, err error) {
	view = picker.ViewSpaces
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			jsonOut = true
		case args[i] == "--view":
			if i+1 >= len(args) {
				return "", fmt.Errorf("hseh list: --view requires spaces or agents")
			}
			i++
			view = args[i]
		case strings.HasPrefix(args[i], "--view="):
			view = strings.TrimPrefix(args[i], "--view=")
		default:
			return "", fmt.Errorf("hseh list: unknown argument %s", args[i])
		}
	}
	if !jsonOut {
		return "", fmt.Errorf("hseh list: --json is required")
	}
	return view, nil
}

// parseRecoverArgs accepts `<definition-id>` plus `--create` and/or `--workspace <id>`;
// space.Recover enforces that exactly one is given.
func parseRecoverArgs(args []string) (definitionID, workspaceID string, create bool, err error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" || strings.HasPrefix(args[0], "-") {
		return "", "", false, fmt.Errorf("hseh recover: definition id is required")
	}
	definitionID = strings.TrimSpace(args[0])
	seenCreate, seenWorkspace := false, false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--create":
			if seenCreate {
				return "", "", false, fmt.Errorf("hseh recover: repeated argument %s", arg)
			}
			seenCreate = true
			create = true
		case arg == "--workspace" || strings.HasPrefix(arg, "--workspace="):
			if seenWorkspace {
				return "", "", false, fmt.Errorf("hseh recover: repeated argument --workspace")
			}
			seenWorkspace = true
			if arg == "--workspace" {
				if i+1 >= len(args) {
					return "", "", false, fmt.Errorf("hseh recover: --workspace requires a live workspace id")
				}
				i++
				arg = "--workspace=" + args[i]
			}
			workspaceID = strings.TrimSpace(strings.TrimPrefix(arg, "--workspace="))
			if workspaceID == "" {
				return "", "", false, fmt.Errorf("hseh recover: --workspace requires a live workspace id")
			}
		default:
			return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
		}
	}
	return definitionID, workspaceID, create, nil
}
