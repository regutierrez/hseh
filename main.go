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
	trace.Event("process.start", "args", strings.Join(os.Args[1:], " "), "since_exec_ms", trace.ProcessAge().Milliseconds(), "unix_ms", time.Now().UnixMilli())
	if err := runHseh(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runHseh(args []string) error {
	if len(args) == 0 {
		return picker.Run(picker.ViewSpaces)
	}
	switch args[0] {
	case "popup":
		view := picker.ViewSpaces
		if len(args) > 1 {
			view = args[1]
		}
		return picker.Run(view)
	case "launch":
		view := picker.ViewSpaces
		if len(args) > 1 {
			view = args[1]
		}
		return runLaunch(view)
	case "list":
		view := picker.ViewSpaces
		jsonOut := false
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--json":
				jsonOut = true
			case "--view":
				if i+1 >= len(args) {
					return fmt.Errorf("hseh list: --view requires spaces or agents")
				}
				i++
				view = args[i]
			default:
				if strings.HasPrefix(args[i], "--view=") {
					view = args[i][len("--view="):]
					continue
				}
				return fmt.Errorf("hseh list: unknown argument %s", args[i])
			}
		}
		if !jsonOut {
			return fmt.Errorf("hseh list: --json is required")
		}
		return runPickerList(view)
	case "switch":
		return runWorkspaceSwitch()
	case "event":
		return runPluginEvent()
	case "open":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("hseh open: definition id is required")
		}
		return runOpenReusableSpace(args[1])
	case "recover":
		return runRecoverReusableSpace(args[1:])
	default:
		return fmt.Errorf("hseh: unknown command %s", args[0])
	}
}
