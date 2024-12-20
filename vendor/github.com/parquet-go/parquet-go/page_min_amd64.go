//go:build !purego

package parquet

//go:noescape
func minInt32(data []int32) int32

//go:noescape
func minInt64(data []int64) int64

//go:noescape
func minUint32(data []uint32) uint32

//go:noescape
func minUint64(data []uint64) uint64

//go:noescape
func minFloat32(data []float32) float32

//go:noescape
func minFloat64(data []float64) float64

//go:noescape
func minBE128(data [][16]byte) []byte
