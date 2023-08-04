// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/errors/error.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package errors

// Error see https://dave.cheney.net/2016/04/07/constant-errors.
type Error string

func (e Error) Error() string { return string(e) }
