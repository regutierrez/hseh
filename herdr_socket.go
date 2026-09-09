package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync/atomic"
)

var herdrRequestSeq atomic.Uint64

// HerdrSocketPath is the current session socket. HERDR_SOCKET_PATH wins.
func HerdrSocketPath() string {
	if path := os.Getenv("HERDR_SOCKET_PATH"); path != "" {
		return path
	}
	return ""
}

// CallHerdrMethod sends one newline-delimited JSON request and decodes the result.
func CallHerdrMethod(method string, params any, result any) (ServerContinuityWitness, error) {
	return CallHerdrMethodContext(context.Background(), method, params, result)
}

// CallHerdrMethodContext closes the socket when ctx is cancelled.
func CallHerdrMethodContext(ctx context.Context, method string, params any, result any) (ServerContinuityWitness, error) {
	socketPath := HerdrSocketPath()
	if socketPath == "" {
		return ServerContinuityWitness{}, fmt.Errorf("hseh herdr socket: HERDR_SOCKET_PATH is not set")
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return ServerContinuityWitness{}, fmt.Errorf("hseh herdr socket: dial %s: %w", socketPath, err)
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
	witness, _ := ReadContinuityWitnessFromConn(conn, socketPath)

	id := fmt.Sprintf("hseh-%d", herdrRequestSeq.Add(1))
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
		return witness, fmt.Errorf("hseh herdr socket: encode %s: %w", method, err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		if ctx.Err() != nil {
			return witness, ctx.Err()
		}
		return witness, fmt.Errorf("hseh herdr socket: write %s: %w", method, err)
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
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
