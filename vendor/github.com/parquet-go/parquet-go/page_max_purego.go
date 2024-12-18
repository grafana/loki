//go:build purego || !amd64

package parquet

import "encoding/binary"

// -----------------------------------------------------------------------------
// TODO: use generics versions of the these functions to reduce the amount of
// code to maintain when we drop compatilibty with Go version older than 1.18.
// -----------------------------------------------------------------------------

func maxInt32(data []int32) (max int32) {
	if len(data) > 0 {
		max = data[0]

		for _, value := range data {
			if value > max {
				max = value
			}
		}
	}
	return max
}

func maxInt64(data []int64) (max int64) {
	if len(data) > 0 {
		max = data[0]

		for _, value := range data {
			if value > max {
				max = value
			}
		}
	}
	return max
}

func maxUint32(data []uint32) (max uint32) {
	if len(data) > 0 {
		max = data[0]

		for _, value := range data {
			if value > max {
				max = value
			}
		}
	}
	return max
}

func maxUint64(data []uint64) (max uint64) {
	if len(data) > 0 {
		max = data[0]

		for _, value := range data {
			if value > max {
				max = value
			}
		}
	}
	return max
}

func maxFloat32(data []float32) (max float32) {
	if len(data) > 0 {
		max = data[0]

		for _, value := range data {
			if value > max {
				max = value
			}
		}
	}
	return max
}

func maxFloat64(data []float64) (max float64) {
	if len(data) > 0 {
		max = data[0]

		for _, value := range data {
			if value > max {
				max = value
			}
		}
	}
	return max
}

func maxBE128(data [][16]byte) (min []byte) {
	if len(data) > 0 {
		m := binary.BigEndian.Uint64(data[0][:8])
		j := 0
		for i := 1; i < len(data); i++ {
			x := binary.BigEndian.Uint64(data[i][:8])
			switch {
			case x > m:
				m, j = x, i
			case x == m:
				y := binary.BigEndian.Uint64(data[i][8:])
				n := binary.BigEndian.Uint64(data[j][8:])
				if y > n {
					m, j = x, i
				}
			}
		}
		min = data[j][:]
	}
	return min
}
