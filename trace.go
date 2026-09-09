package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HSEH_TRACE names an append-only file that receives one line per timed event.
// Unset (the default) makes every hook a nil check. Lines look like:
//
//	+123.456ms socket.call dur=0.482ms method=session.snapshot bytes=16645
//
// The leading offset is relative to process start. Summaries are meant for
// sort and awk, not a dashboard.
var (
	traceStart = time.Now()
	traceMu    sync.Mutex
	traceOut   *os.File
	traceInit  sync.Once
)

// traceProcessAge reports how long the process had existed when called, from
// /proc on Linux (10ms resolution). It exposes work done before main, such as
// Bubble Tea's package init querying the terminal for its background color,
// which blocks for termenv's 5s timeout on terminals that never answer.
// Returns -1 when unavailable.
func traceProcessAge() time.Duration {
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return -1
	}
	uptime, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return -1
	}
	text := string(stat)
	closeParen := strings.LastIndex(text, ")")
	if closeParen < 0 {
		return -1
	}
	fields := strings.Fields(text[closeParen+2:])
	if len(fields) < 20 {
		return -1
	}
	startTicks, err := strconv.ParseFloat(fields[19], 64)
	if err != nil {
		return -1
	}
	upSeconds, err := strconv.ParseFloat(strings.Fields(string(uptime))[0], 64)
	if err != nil {
		return -1
	}
	const clockTicksPerSecond = 100
	return time.Duration((upSeconds - startTicks/clockTicksPerSecond) * float64(time.Second))
}

func traceEnabled() bool {
	traceInit.Do(func() {
		path := os.Getenv("HSEH_TRACE")
		if path == "" {
			return
		}
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		traceOut = file
	})
	return traceOut != nil
}

// traceEvent logs an instantaneous event. kv pairs alternate key, value.
func traceEvent(name string, kv ...any) {
	if !traceEnabled() {
		return
	}
	traceWrite(name, -1, kv)
}

// traceSpan starts a timed span; call the returned func when the work is done.
// Extra kv pairs passed at completion are appended to the same line.
func traceSpan(name string, kv ...any) func(more ...any) {
	if !traceEnabled() {
		return func(...any) {}
	}
	start := time.Now()
	return func(more ...any) {
		traceWrite(name, time.Since(start), append(append([]any{}, kv...), more...))
	}
}

func traceWrite(name string, dur time.Duration, kv []any) {
	var b strings.Builder
	fmt.Fprintf(&b, "+%.3fms %s", float64(time.Since(traceStart).Microseconds())/1000, name)
	if dur >= 0 {
		fmt.Fprintf(&b, " dur=%.3fms", float64(dur.Microseconds())/1000)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, " %v=%v", kv[i], kv[i+1])
	}
	b.WriteByte('\n')
	traceMu.Lock()
	defer traceMu.Unlock()
	_, _ = traceOut.WriteString(b.String())
}
