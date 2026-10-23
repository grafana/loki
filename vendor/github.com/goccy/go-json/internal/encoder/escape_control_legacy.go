//go:build !go1.22

package encoder

// backspaceEscape and formFeedEscape are the escapes of the backspace and the form feed: encoding/json before Go
// 1.22 writes them as the other control characters, by their code.
const (
	backspaceEscape = `\u0008`
	formFeedEscape  = `\u000c`
)
