package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/trace"
)

var requestSeq atomic.Uint64

// readContinuityWitness is swapped by tests to observe when the witness is read relative to the request write.
var readContinuityWitness = ReadContinuityWitnessFromConn

// Call sends one newline-delimited JSON request and decodes the result.
func Call(method string, params any, result any) (ContinuityWitness, error) {
	return CallContext(context.Background(), method, params, result)
}

// CallContext closes the socket when ctx is cancelled.
func CallContext(ctx context.Context, method string, params any, result any) (ContinuityWitness, error) {
	socketPath := config.SocketPath()
	if socketPath == "" {
		return ContinuityWitness{}, fmt.Errorf("hseh herdr socket: HERDR_SOCKET_PATH is not set")
	}
	span := trace.Span("socket.call", "method", method)
	var replyBytes int
	defer func() { span("bytes", replyBytes) }()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return ContinuityWitness{}, fmt.Errorf("hseh herdr socket: dial %s: %w", socketPath, err)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	defer func() {
		close(done)
		conn.Close()
	}()
	// Write the request before anything else touches the connection. Herdr answers
	// within a millisecond only when the request arrives right after connect; a
	// short pause (such as the /proc reads below) pushes the reply about 100ms
	// out to the server's next poll tick.
	id := fmt.Sprintf("hseh-%d", requestSeq.Add(1))
	request := struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{ID: id, Method: method, Params: params}
	if params == nil {
		request.Params = map[string]any{}
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return ContinuityWitness{}, fmt.Errorf("hseh herdr socket: encode %s: %w", method, err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		if ctx.Err() != nil {
			return ContinuityWitness{}, ctx.Err()
		}
		return ContinuityWitness{}, fmt.Errorf("hseh herdr socket: write %s: %w", method, err)
	}

	// Peer credentials are fixed at connect, so the witness reads equally well
	// while the reply is in flight or after the peer has closed.
	witnessSpan := trace.Span("socket.witness", "method", method)
	witness, _ := readContinuityWitness(conn, socketPath)
	witnessSpan()

	readSpan := trace.Span("socket.reply", "method", method)
	line, err := readReply(ctx, conn, socketPath, method, payload)
	replyBytes = len(line)
	readSpan("bytes", replyBytes)
	if err != nil {
		if ctx.Err() != nil {
			return witness, ctx.Err()
		}
		return witness, fmt.Errorf("hseh herdr socket: read %s: %w", method, err)
	}
	var envelope struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		return witness, fmt.Errorf("hseh herdr socket: decode %s: %w", method, err)
	}
	if envelope.Error != nil {
		return witness, fmt.Errorf("hseh herdr socket: %s: %s %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	if result == nil {
		return witness, nil
	}
	if len(envelope.Result) == 0 {
		return witness, fmt.Errorf("hseh herdr socket: %s response did not contain a result", method)
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return witness, fmt.Errorf("hseh herdr socket: decode %s result: %w", method, err)
	}
	return witness, nil
}

// hedgeDelay is how long a read-only request may go unanswered before a
// second connection resends it. Herdr normally replies in under a millisecond;
// a request that misses its accept window waits for the server's ~100ms tick.
const hedgeDelay = 2 * time.Millisecond

type hedgeKey struct{}

// WithHedge marks a call as latency-visible: its reply may be hedged. Only
// calls a person is waiting on qualify (first snapshot, selection-change preview,
// popup open). Background polls are never hedged; a late refresh is invisible
// and the duplicate would only cost Herdr a second round of work.
func WithHedge(ctx context.Context) context.Context {
	return context.WithValue(ctx, hedgeKey{}, true)
}

// Hedged reports whether ctx carries the WithHedge marker. It lets callers
// (and tests) confirm a latency-visible read was marked before it went out.
func Hedged(ctx context.Context) bool {
	allowed, _ := ctx.Value(hedgeKey{}).(bool)
	return allowed
}

func hedgeAllowed(ctx context.Context, method string) bool {
	allowed, _ := ctx.Value(hedgeKey{}).(bool)
	return allowed && idempotentMethod(method)
}

// idempotentMethod reports methods that are safe to send twice.
// plugin.pane.open qualifies because Herdr refuses a second popup with ui_busy
// while the first is open; OpenPluginPopup treats that refusal as success.
func idempotentMethod(method string) bool {
	switch method {
	case "session.snapshot", "pane.read", "plugin.pane.open":
		return true
	}
	return false
}

type reply struct {
	line []byte
	err  error
	conn net.Conn
}

// readReply reads one reply line. For hedge-allowed calls (see WithHedge)
// it hedges: if the first connection is silent past hedgeDelay, the request
// is resent on a fresh connection and the first reply wins. The loser is closed.
func readReply(ctx context.Context, conn net.Conn, socketPath, method string, payload []byte) ([]byte, error) {
	if !hedgeAllowed(ctx, method) {
		return bufio.NewReader(conn).ReadBytes('\n')
	}
	replies := make(chan reply, 2)
	read := func(c net.Conn) {
		line, err := bufio.NewReader(c).ReadBytes('\n')
		replies <- reply{line: line, err: err, conn: c}
	}
	go read(conn)
	timer := time.NewTimer(hedgeDelay)
	defer timer.Stop()
	var hedge net.Conn
	closeLoser := func(winner net.Conn) {
		if hedge != nil && hedge != winner {
			hedge.Close()
		}
	}
	for {
		select {
		case r := <-replies:
			if r.err != nil && r.conn == conn && hedge != nil {
				// Primary failed after the hedge went out; wait for the hedge.
				conn = nil
				continue
			}
			if r.err != nil && r.conn == hedge && conn != nil {
				hedge = nil
				continue
			}
			closeLoser(r.conn)
			trace.Event("socket.hedge", "method", method, "winner", map[bool]string{true: "hedge", false: "primary"}[r.conn == hedge && hedge != nil])
			return r.line, r.err
		case <-timer.C:
			if hedge != nil {
				continue
			}
			var d net.Dialer
			c, err := d.DialContext(ctx, "unix", socketPath)
			if err != nil {
				continue
			}
			if _, err := c.Write(append(payload, '\n')); err != nil {
				c.Close()
				continue
			}
			hedge = c
			go read(c)
		case <-ctx.Done():
			if hedge != nil {
				hedge.Close()
			}
			return nil, ctx.Err()
		}
	}
}
