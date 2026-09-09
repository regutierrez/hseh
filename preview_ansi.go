package main

import (
	"strings"
	"unicode/utf8"
)

// AllowVisiblePreviewANSI keeps SGR color sequences and drops cursor, OSC,
// clipboard, C1, and other terminal-control effects. This is not a terminal emulator.
func AllowVisiblePreviewANSI(input string) string {
	return filterTerminalBytes(input, true)
}

// StripTerminalControls removes terminal controls for plain JSON rows and query text.
func StripTerminalControls(input string) string {
	return filterTerminalBytes(input, false)
}

func filterTerminalBytes(input string, keepSGR bool) string {
	var out strings.Builder
	out.Grow(len(input))
	for i := 0; i < len(input); {
		r, size := utf8.DecodeRuneInString(input[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		if r == 0x1b {
			consumed, keep := consumeEscape(input[i:], keepSGR)
			out.WriteString(keep)
			i += consumed
			continue
		}
		if r == 0x9b {
			consumed, keep := consumeCSIBody(input[i+size:], keepSGR)
			out.WriteString(keep)
			i += size + consumed
			continue
		}
		if r == 0x9d || r == 0x90 || r == 0x98 || r == 0x9e || r == 0x9f {
			i += size + consumeStringTerminator(input[i+size:])
			continue
		}
		if r == 0x9c || r == 0x07 || r == 0x18 || r == 0x1a {
			i += size
			continue
		}
		if r >= 0x80 && r <= 0x9f {
			i += size
			continue
		}
		if r == '\r' {
			next := i + size
			if next < len(input) && input[next] == '\n' {
				i = next
				continue
			}
			i += size
			continue
		}
		if r == '\t' {
			out.WriteByte(' ')
			i += size
			continue
		}
		if r < 0x20 && r != '\n' {
			i += size
			continue
		}
		out.WriteString(input[i : i+size])
		i += size
	}
	return out.String()
}

func consumeEscape(input string, keepSGR bool) (int, string) {
	if len(input) < 2 {
		return len(input), ""
	}
	switch input[1] {
	case '[':
		n, keep := consumeCSIBody(input[2:], keepSGR)
		return 2 + n, keep
	case ']', 'P', 'X', '^', '_':
		return 2 + consumeStringTerminator(input[2:]), ""
	default:
		return 2, ""
	}
}

func consumeCSIBody(input string, keepSGR bool) (int, string) {
	i := 0
	for i < len(input) {
		b := input[i]
		if b >= 0x40 && b <= 0x7e {
			i++
			if keepSGR && b == 'm' {
				return i, "\x1b[" + input[:i]
			}
			return i, ""
		}
		i++
	}
	return len(input), ""
}

func consumeStringTerminator(input string) int {
	i := 0
	for i < len(input) {
		r, size := utf8.DecodeRuneInString(input[i:])
		if r == 0x07 || r == 0x9c {
			return i + size
		}
		if r == 0x1b && i+1 < len(input) && input[i+1] == '\\' {
			return i + 2
		}
		i += size
	}
	return len(input)
}
