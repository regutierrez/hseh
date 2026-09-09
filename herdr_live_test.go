package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFocusHerdrAgentContextSyncsReturnedTab(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var focusedTab string
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var req struct {
					ID     string          `json:"id"`
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if json.Unmarshal(line, &req) != nil {
					return
				}
				var result any
				switch req.Method {
				case "agent.focus":
					mu.Lock()
					methods = append(methods, req.Method)
					mu.Unlock()
					agent := HerdrAgentRow{HerdrPaneRow: HerdrPaneRow{
						PaneID:      "w1:p2",
						TabID:       "w1:t2",
						WorkspaceID: "w1",
						Agent:       "pi",
					}}
					result = herdrAgentFocusEnvelope{Type: "agent_info", Agent: &agent}
				case "tab.focus":
					var params struct {
						TabID string `json:"tab_id"`
					}
					_ = json.Unmarshal(req.Params, &params)
					mu.Lock()
					methods = append(methods, req.Method)
					focusedTab = params.TabID
					mu.Unlock()
					result = map[string]any{"type": "ok"}
				default:
					result = map[string]any{"type": "ok"}
				}
				payload, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
				_, _ = c.Write(append(payload, '\n'))
			}(conn)
		}
	}()
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	if err := FocusHerdrAgentContext(context.Background(), "w1:p2"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 2 || methods[0] != "agent.focus" || methods[1] != "tab.focus" {
		t.Fatalf("methods %v", methods)
	}
	if focusedTab != "w1:t2" {
		t.Fatalf("tab.focus used %q, want returned agent tab_id", focusedTab)
	}
}

func TestFocusHerdrAgentContextRejectsMissingTabID(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var req struct {
					ID     string `json:"id"`
					Method string `json:"method"`
				}
				if json.Unmarshal(line, &req) != nil {
					return
				}
				mu.Lock()
				methods = append(methods, req.Method)
				mu.Unlock()
				agent := HerdrAgentRow{HerdrPaneRow: HerdrPaneRow{PaneID: "w1:p2", WorkspaceID: "w1", Agent: "pi"}}
				result := any(herdrAgentFocusEnvelope{Type: "agent_info", Agent: &agent})
				payload, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
				_, _ = c.Write(append(payload, '\n'))
			}(conn)
		}
	}()
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	err = FocusHerdrAgentContext(context.Background(), "w1:p2")
	if err == nil || !strings.Contains(err.Error(), "hseh focus: agent.focus missing tab_id") {
		t.Fatalf("got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 1 || methods[0] != "agent.focus" {
		t.Fatalf("guessed fallback after malformed agent.focus: %v", methods)
	}
}
