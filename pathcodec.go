package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// quoteRune marks an escaped byte in the index. A filesystem path is an
// arbitrary byte string on Linux, but a JSON string has to be valid UTF-8, so
// the bytes that do not decode are quoted the way rclone does it: 0xFE is
// stored as ‛FE. Valid names, which is all but a handful in practice, are
// stored verbatim.
//
// It is deliberately not NUL. A NUL survives in a Go string but truncates in
// any C consumer, and it never appears in a path to begin with, so using it
// would introduce a hazard that the data does not otherwise have.
//
// U+201B SINGLE HIGH-REVERSED-9 QUOTATION MARK, the rune rclone uses for the
// same job.
const quoteRune = '‛'

// needsEncoding reports whether a path cannot be stored verbatim.
func needsEncoding(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsRune(s, quoteRune)
}

// encodePath makes a filesystem path storable as a JSON string. Any path that
// is already valid UTF-8 and free of the quote rune comes back unchanged.
func encodePath(s string) string {
	if !needsEncoding(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			// A byte that starts no valid sequence. Quoting it alone keeps
			// the decoded length recoverable.
			fmt.Fprintf(&b, "%c%02X", quoteRune, s[i])
			i++
		case r == quoteRune:
			// The marker itself, doubled.
			b.WriteRune(quoteRune)
			b.WriteRune(quoteRune)
			i += size
		default:
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}

// decodePath is the inverse of encodePath.
func decodePath(s string) string {
	if !strings.ContainsRune(s, quoteRune) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != quoteRune {
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		rest := s[i+size:]
		if strings.HasPrefix(rest, string(quoteRune)) {
			b.WriteRune(quoteRune)
			i += size * 2
			continue
		}
		if len(rest) >= 2 {
			if v, err := strconv.ParseUint(rest[:2], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += size + 2
				continue
			}
		}
		// A quote rune that is not followed by a valid escape is literal.
		// encodePath never writes one, but a hand-edited index might.
		b.WriteRune(quoteRune)
		i += size
	}
	return b.String()
}

// encodeKeys rewrites object names for storage. Most indexes need no rewriting
// at all, in which case the input map is returned as-is.
func encodeKeys[V any](in map[string]V) map[string]V {
	needs := false
	for k := range in {
		if needsEncoding(k) {
			needs = true
			break
		}
	}
	if !needs {
		return in
	}
	out := make(map[string]V, len(in))
	for k, v := range in {
		out[encodePath(k)] = v
	}
	return out
}

// decodeKeys is the inverse of encodeKeys.
func decodeKeys[V any](raw map[string]V) map[string]V {
	needs := false
	for k := range raw {
		if strings.ContainsRune(k, quoteRune) {
			needs = true
			break
		}
	}
	if !needs {
		return raw
	}
	out := make(map[string]V, len(raw))
	for k, v := range raw {
		out[decodePath(k)] = v
	}
	return out
}

// Path is a filesystem path held in the index. Paths hold raw bytes in memory
// and are quoted only on the way into JSON, like the map keys are.
type Path string

// MarshalText implements encoding.TextMarshaler.
func (p Path) MarshalText() ([]byte, error) {
	return []byte(encodePath(string(p))), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (p *Path) UnmarshalText(text []byte) error {
	*p = Path(decodePath(string(text)))
	return nil
}
