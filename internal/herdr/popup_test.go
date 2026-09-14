package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestOpenPluginPopupBusyIsTypedSuccessAndUnknownMethodFails(t *testing.T) {
	server := &hsehtest.Server{}
	socket, _ := hsehtest.Start(t, server)
	t.Setenv("HERDR_SOCKET_PATH", socket)

	_, err := callContext(context.Background(), "no.such.method", map[string]any{}, nil)
	var callErr *CallError
	if !errors.As(err, &callErr) || callErr.Code != "unknown_method" {
		t.Fatalf("unknown method: %v", err)
	}

	if err := OpenPluginPopup("spaces"); err != nil {
		t.Fatal(err)
	}
	_, err = callContext(context.Background(), "plugin.pane.open", map[string]any{
		"plugin_id":  "hseh",
		"entrypoint": "spaces",
		"placement":  "popup",
	}, nil)
	if !errors.As(err, &callErr) || callErr.Code != "ui_busy" {
		t.Fatalf("second plugin.pane.open: %v", err)
	}
	if err := OpenPluginPopup("spaces"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenPluginPopupDoesNotTreatBusySubstringAsSuccess(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
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
	t.Setenv("HERDR_SOCKET_PATH", socketPath)

	err = OpenPluginPopup("spaces")
	var callErr *CallError
	if err == nil || !errors.As(err, &callErr) || callErr.Code != "failed" {
		t.Fatalf("message substring must not count as busy: %v", err)
	}
}
