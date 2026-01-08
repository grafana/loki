package fair

import "strings"

// A Scope denotes a named area within a [Queue] where items may be queued.
type Scope []string

// String returns the full name of the scope, with each element separated by a
// slash.
func (s Scope) String() string {
	return strings.Join(s, "/")
}

// Name returns the local name of the scope (last element).
func (s Scope) Name() string {
	return s[len(s)-1]
}
