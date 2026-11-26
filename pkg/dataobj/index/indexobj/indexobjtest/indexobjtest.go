// Package indexobjtest provides helpers for tests that use the index object
// builders.
package indexobjtest

// NoBloomDenyList is a bloom deny list that denies no column.
type NoBloomDenyList struct{}

// DataObjBloomDenyList returns no column.
func (NoBloomDenyList) DataObjBloomDenyList(string) []string { return nil }
