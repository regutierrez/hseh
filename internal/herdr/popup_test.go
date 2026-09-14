package herdr_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestOpenPluginPopupBusyIsTypedSuccessAndUnknownMethodFails(t *testing.T) {
	server := &hsehtest.Server{}
	socket, _ := hsehtest.Start(t, server)
	t.Setenv("HERDR_SOCKET_PATH", socket)

	code := rawErrorCode(t, socket, "no.such.method")
	if code != "unknown_method" {
		t.Fatalf("unknown method code %q, want unknown_method", code)
	}

	if err := herdr.OpenPluginPopup("spaces"); err != nil {
		t.Fatal(err)
	}
	code = rawErrorCode(t, socket, "plugin.pane.open")
	if code != "ui_busy" {
		t.Fatalf("second plugin.pane.open code %q, want ui_busy", code)
	}
	if err := herdr.OpenPluginPopup("spaces"); err != nil {
		t.Fatal(err)
	}
}

func rawErrorCode(t *testing.T, socket, method string) string {
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

func TestOpenPluginPopupMatchesBusyCodeNotMessage(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socket)
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
				if _, err := bufio.NewReader(c).ReadBytes('\n'); err != nil {
					return
				}
				payload, _ := json.Marshal(map[string]any{
					"id":    "x",
					"error": map[string]any{"code": "failed", "message": "ui_busy"},
				})
				_, _ = c.Write(append(payload, '\n'))
			}(conn)
		}
	}()
	t.Setenv("HERDR_SOCKET_PATH", socket)

	err = herdr.OpenPluginPopup("spaces")
	var callErr *herdr.CallError
	if err == nil || !errors.As(err, &callErr) || callErr.Code != "failed" {
		t.Fatalf("message substring must not count as busy: %v", err)
	}
}
