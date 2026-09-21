package hsehtest

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
)

func TestUnknownMethodAndBusyPopup(t *testing.T) {
	socket, _ := Start(t, &Server{})
	if code := rpcError(t, socket, "no.such.method"); code != "unknown_method" {
		t.Fatalf("unknown method code %q, want unknown_method", code)
	}
	if code := rpcError(t, socket, "plugin.pane.open"); code != "" {
		t.Fatalf("first plugin.pane.open code %q, want success", code)
	}
	if code := rpcError(t, socket, "plugin.pane.open"); code != "ui_busy" {
		t.Fatalf("second plugin.pane.open code %q, want ui_busy", code)
	}
}

func rpcError(t *testing.T, socket, method string) string {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload, err := json.Marshal(map[string]any{"id": "t1", "method": method, "params": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == nil {
		return ""
	}
	return env.Error.Code
}
