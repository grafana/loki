// Package dhp1 holds the small, literal pieces of the DHP/1 wire protocol
// that client and server each encode/decode independently but must still
// agree on bit-for-bit -- the mode-field bytes a THROTTLE entry can carry,
// and which runes the protocol's own tokenizers (strings.Fields, used by
// both sides, plus a literal '|' split) treat as delimiters. See the
// top-level README's "DeadHorse Protocol reference" section for the full
// grammar; this package exists so the two independent implementations of
// it can't silently drift on these specific values, the way two
// independently-typed copies of the same literal eventually do.
package dhp1

import "unicode"

// ModeReal and ModePeek are a THROTTLE entry's mode-field values: "R" (real
// -- consume on success) or "P" (peek -- never mutates state). See
// deadhorse.RequestEntry.Peek.
const (
	ModeReal = "R"
	ModePeek = "P"
)

// IsKeyDelimiter reports whether r is a rune the wire protocol's own
// tokenizers treat as a delimiter: any Unicode whitespace (what
// strings.Fields splits on), or '|' (what a THROTTLE/RESULT entry's own
// fields split on). A key containing e.g. '\v' -- Unicode whitespace, but
// outside a narrower ASCII-only blacklist -- would otherwise pass
// validation only to get split into two wire tokens later, desyncing
// whatever batch it was part of.
func IsKeyDelimiter(r rune) bool {
	return unicode.IsSpace(r) || r == '|'
}
