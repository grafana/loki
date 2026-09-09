//go:build !(goexperiment.simd && amd64)

package jsonlite

// simdStage1 reports whether the vectorized structural indexer is available.
func simdStage1() bool { return false }

// structuralIndex scans s and appends emitted positions to index, returning
// the index, document-level flags, and any string-level validation error.
func structuralIndex(s string, index []uint32) ([]uint32, stage1Flags, error) {
	return structuralIndexPortable(s, index)
}
