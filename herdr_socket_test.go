package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// Herdr replies about 100ms late when the request does not arrive right after
// connect. The client must therefore write the request before doing any other
// work on the connection, including the continuity witness reads.
func TestCallHerdrMethodWritesRequestBeforeWitnessRead(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
			return
		}
		close(received)
		_, _ = conn.Write([]byte(`{"id":"hseh-1","result":{"ok":true}}` + "\n"))
	}()

	original := readContinuityWitness
	t.Cleanup(func() { readContinuityWitness = original })
	witnessAfterRequest := false
	readContinuityWitness = func(conn net.Conn, path string) (ServerContinuityWitness, error) {
		select {
		case <-received:
			witnessAfterRequest = true
		case <-time.After(2 * time.Second):
		}
		return ServerContinuityWitness{SocketPath: path, PeerPID: 1, PeerStartTime: "1"}, nil
	}

	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	var result map[string]any
	witness, err := CallHerdrMethodContext(context.Background(), "session.snapshot", map[string]any{}, &result)
	if err != nil {
		t.Fatal(err)
	}
	if !witnessAfterRequest {
		t.Fatal("continuity witness was read before the request reached the server")
	}
	if witness.PeerPID != 1 || result["ok"] != true {
		t.Fatalf("witness or result lost: %+v %v", witness, result)
	}
}

// The witness must still be read even when the peer closes right after replying.
func TestCallHerdrMethodWitnessSurvivesPeerClose(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = conn.Write([]byte(`{"id":"hseh-1","result":{}}` + "\n"))
		conn.Close()
	}()
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	var result json.RawMessage
	witness, err := CallHerdrMethodContext(context.Background(), "workspace.focus", map[string]any{"workspace_id": "w1"}, &result)
	if err != nil {
		t.Fatal(err)
	}
	if witness.PeerPID == 0 || witness.SocketPath != socketPath {
		t.Fatalf("witness not read from a closing peer: %+v", witness)
	}
}

// A first connection that Herdr leaves waiting on its slow tick must not delay
// read-only calls: a second connection resends the request and its reply wins.
func TestCallHerdrMethodHedgesSlowIdempotentReply(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	var connCount int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			n := atomic.AddInt32(&connCount, 1)
			go func(c net.Conn, n int32) {
				defer c.Close()
				if _, err := bufio.NewReader(c).ReadBytes('\n'); err != nil {
					return
				}
				if n == 1 {
					time.Sleep(300 * time.Millisecond) // the slow tick
				}
				_, _ = c.Write([]byte(`{"id":"x","result":{"conn":` + strconv.Itoa(int(n)) + `}}` + "\n"))
			}(conn, n)
		}
	}()
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	var result map[string]any
	start := time.Now()
	if _, err := CallHerdrMethodContext(withHerdrHedge(context.Background()), "session.snapshot", map[string]any{}, &result); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("hedge did not rescue the slow reply: %v", elapsed)
	}
	if result["conn"] != float64(2) {
		t.Fatalf("expected the hedged connection to win, got %v", result)
	}
	// Non-idempotent methods must never be resent, even when hedging is allowed.
	atomic.StoreInt32(&connCount, 0)
	var raw json.RawMessage
	if _, err := CallHerdrMethodContext(withHerdrHedge(context.Background()), "workspace.focus", map[string]any{"workspace_id": "w1"}, &raw); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&connCount); got != 1 {
		t.Fatalf("workspace.focus opened %d connections, want 1", got)
	}
	// Background polls (no hedge marker) wait for the slow reply rather than duplicating work.
	atomic.StoreInt32(&connCount, 0)
	if _, err := CallHerdrMethodContext(context.Background(), "session.snapshot", map[string]any{}, &result); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&connCount); got != 1 {
		t.Fatalf("unmarked session.snapshot opened %d connections, want 1", got)
	}
}
