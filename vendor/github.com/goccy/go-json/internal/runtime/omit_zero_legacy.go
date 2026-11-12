//go:build !go1.24

package runtime

// omitZeroOption is whether the omitzero option of a field is honored: encoding/json of a Go older than 1.24
// ignores it.
const omitZeroOption = false
