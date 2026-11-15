package errors

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ErrInvalidUTF8 reports invalid UTF-8 in a JSON string: jsontext and the v2 json package report it by this error.
var ErrInvalidUTF8 = errors.New("invalid UTF-8")

// maxShownPointer is the length over which the pointer of an error is shown shortened: its start and its end,
// of about half of it each, cut before a token if they can be.
const maxShownPointer = 100

// ShortPointer shortens a long pointer. The part which is left out is shown by an ellipsis for the end of the
// token which the start cuts, one for the whole tokens, and one for the start of the token which the end cuts.
func ShortPointer(p string) string {
	if len(p) <= maxShownPointer {
		return p
	}
	half := maxShownPointer / 2
	head := strings.LastIndexByte(p[1:half], '/') + 1
	if head <= 0 {
		for head = half; !utf8.RuneStart(p[head]); head-- {
		}
	}
	from := max(len(p)-half, head+1)
	tail := strings.IndexByte(p[from:], '/')
	tailCut := tail < 0 // the end starts within a token
	if tailCut {
		for tail = len(p) - half; !utf8.RuneStart(p[tail]); tail++ {
		}
	} else {
		tail += from
	}
	out := p[:head]
	cut := p[head:tail]
	first, last := strings.IndexByte(cut, '/'), strings.LastIndexByte(cut, '/')
	if first != 0 {
		out += "…" // the end of a token
	}
	if first >= 0 && (last > first || !tailCut) {
		out += "/…" // whole tokens
	}
	if tailCut && first >= 0 {
		out += "/"
		if last < len(cut)-1 {
			out += "…" // the start of a token
		}
	}
	return out + p[tail:]
}
