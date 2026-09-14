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

func TestOpenPluginPopupTreatsBusyCodeAsSuccess(t *testing.T) {
	socket, _ := hsehtest.Start(t, &hsehtest.Server{})
	t.Setenv("HERDR_SOCKET_PATH", socket)
	if err := herdr.OpenPluginPopup("spaces"); err != nil {
		t.Fatal(err)
	}
	if err := herdr.OpenPluginPopup("spaces"); err != nil {
		t.Fatal(err)
	}
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
