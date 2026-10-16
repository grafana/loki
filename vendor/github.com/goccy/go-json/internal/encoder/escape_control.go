//go:build go1.22

package encoder

// backspaceEscape and formFeedEscape are the escapes of the backspace and the form feed: encoding/json since Go
// 1.22 writes them as \b and \f.
const (
	backspaceEscape = `\b`
	formFeedEscape  = `\f`
)
