//go:build debug

package debug

// SetFinalizer is a no-op when the debug tag is specified.
func SetFinalizer(interface{}, interface{}) {}
