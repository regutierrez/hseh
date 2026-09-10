package trace

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/regutierrez/hseh/internal/config"
)

var (
	processStart = time.Now()
	mu           sync.Mutex
	out          *os.File
	initOnce     sync.Once
)

// ProcessAge reports how long the process had existed when called, from
// /proc on Linux (10ms resolution). It exposes work done before main, such as
// Bubble Tea's package init querying the terminal for its background color,
// which blocks for termenv's 5s timeout on terminals that never answer.
// ok is false where /proc is unavailable.
func ProcessAge() (age time.Duration, ok bool) {
	startText, err := ProcStartTicks(os.Getpid())
	if err != nil {
		return 0, false
	}
	uptime, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	startTicks, err := strconv.ParseFloat(startText, 64)
	if err != nil {
		return 0, false
	}
	upSeconds, err := strconv.ParseFloat(strings.Fields(string(uptime))[0], 64)
	if err != nil {
		return 0, false
	}
	const clockTicksPerSecond = 100
	return time.Duration((upSeconds - startTicks/clockTicksPerSecond) * float64(time.Second)), true
}

// ProcStartTicks returns the raw starttime field of /proc/<pid>/stat: clock
// ticks since boot when the process started, which together with the pid
// identifies one process incarnation.
func ProcStartTicks(pid int) (string, error) {
	path := "/proc/" + strconv.Itoa(pid) + "/stat"
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(payload)
	closeParen := strings.LastIndex(text, ")")
	if closeParen < 0 || closeParen+2 >= len(text) {
		return "", fmt.Errorf("%s: missing comm", path)
	}
	fields := strings.Fields(text[closeParen+2:])
	if len(fields) < 20 {
		return "", fmt.Errorf("%s: too short", path)
	}
	return fields[19], nil
}

func Enabled() bool {
	initOnce.Do(func() {
		path := os.Getenv("HSEH_TRACE")
		if path == "" {
			// Herdr spawns plugin processes with its own environment, so the
			// plugin config file is the way to trace popups and actions.
			settings, _ := config.Load()
			path = settings.TraceFile
		}
		if path == "" {
			return
		}
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "hseh trace: %v\n", err)
			return
		}
		out = file
	})
	return out != nil
}

// Event logs an instantaneous event. kv pairs alternate key, value.
func Event(name string, kv ...any) {
	if !Enabled() {
		return
	}
	write(name, -1, kv)
}

// Span starts a timed span; call the returned func when the work is done.
// Extra kv pairs passed at completion are appended to the same line.
func Span(name string, kv ...any) func(more ...any) {
	if !Enabled() {
		return func(...any) {}
	}
	start := time.Now()
	return func(more ...any) {
		write(name, time.Since(start), append(append([]any{}, kv...), more...))
	}
}

// write emits one line:
//
//	+123.456ms socket.call dur=0.482ms method=session.snapshot bytes=16645
//
// The leading offset is relative to process start. Summaries are meant for
// sort and awk, not a dashboard.
func write(name string, dur time.Duration, kv []any) {
	var b strings.Builder
	fmt.Fprintf(&b, "+%.3fms %s", float64(time.Since(processStart).Microseconds())/1000, name)
	if dur >= 0 {
		fmt.Fprintf(&b, " dur=%.3fms", float64(dur.Microseconds())/1000)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, " %v=%v", kv[i], kv[i+1])
	}
	b.WriteByte('\n')
	mu.Lock()
	defer mu.Unlock()
	_, _ = out.WriteString(b.String())
}
