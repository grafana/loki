//go:build !purego

package parquet

//go:noescape
func orderOfInt32(data []int32) int

//go:noescape
func orderOfInt64(data []int64) int

//go:noescape
func orderOfUint32(data []uint32) int

//go:noescape
func orderOfUint64(data []uint64) int

//go:noescape
func orderOfFloat32(data []float32) int

//go:noescape
func orderOfFloat64(data []float64) int
