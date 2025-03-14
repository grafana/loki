//go:build purego || !amd64

package parquet

import "encoding/binary"

// -----------------------------------------------------------------------------
// TODO: use generics versions of the these functions to reduce the amount of
// code to maintain when we drop compatilibty with Go version older than 1.18.
// -----------------------------------------------------------------------------

func minInt32(data []int32) (min int32) {
	if len(data) > 0 {
		min = data[0]

		for _, value := range data {
			if value < min {
				min = value
			}
		}
	}
	return min
}

func minInt64(data []int64) (min int64) {
	if len(data) > 0 {
		min = data[0]

		for _, value := range data {
			if value < min {
				min = value
			}
		}
	}
	return min
}

func minUint32(data []uint32) (min uint32) {
	if len(data) > 0 {
		min = data[0]

		for _, value := range data {
			if value < min {
				min = value
			}
		}
	}
	return min
}

func minUint64(data []uint64) (min uint64) {
	if len(data) > 0 {
		min = data[0]

		for _, value := range data {
			if value < min {
				min = value
			}
		}
	}
	return min
}

func minFloat32(data []float32) (min float32) {
	if len(data) > 0 {
		min = data[0]

		for _, value := range data {
			if value < min {
				min = value
			}
		}
	}
	return min
}

func minFloat64(data []float64) (min float64) {
	if len(data) > 0 {
		min = data[0]

		for _, value := range data {
			if value < min {
				min = value
			}
		}
	}
	return min
}

func minBE128(data [][16]byte) (min []byte) {
	if len(data) > 0 {
		m := binary.BigEndian.Uint64(data[0][:8])
		j := 0
		for i := 1; i < len(data); i++ {
			x := binary.BigEndian.Uint64(data[i][:8])
			switch {
			case x < m:
				m, j = x, i
			case x == m:
				y := binary.BigEndian.Uint64(data[i][8:])
				n := binary.BigEndian.Uint64(data[j][8:])
				if y < n {
					m, j = x, i
				}
			}
		}
		min = data[j][:]
	}
	return min
}
