//go:build purego || !amd64

package parquet

import (
	"encoding/binary"
)

func boundsInt32(data []int32) (min, max int32) {
	if len(data) > 0 {
		min = data[0]
		max = data[0]

		for _, v := range data[1:] {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	return min, max
}

func boundsInt64(data []int64) (min, max int64) {
	if len(data) > 0 {
		min = data[0]
		max = data[0]

		for _, v := range data[1:] {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	return min, max
}

func boundsUint32(data []uint32) (min, max uint32) {
	if len(data) > 0 {
		min = data[0]
		max = data[0]

		for _, v := range data[1:] {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	return min, max
}

func boundsUint64(data []uint64) (min, max uint64) {
	if len(data) > 0 {
		min = data[0]
		max = data[0]

		for _, v := range data[1:] {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	return min, max
}

func boundsFloat32(data []float32) (min, max float32) {
	if len(data) > 0 {
		min = data[0]
		max = data[0]

		for _, v := range data[1:] {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	return min, max
}

func boundsFloat64(data []float64) (min, max float64) {
	if len(data) > 0 {
		min = data[0]
		max = data[0]

		for _, v := range data[1:] {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
	}
	return min, max
}

func boundsBE128(data [][16]byte) (min, max []byte) {
	if len(data) > 0 {
		minHi := binary.BigEndian.Uint64(data[0][:8])
		maxHi := minHi
		minIndex := 0
		maxIndex := 0
		for i := 1; i < len(data); i++ {
			hi := binary.BigEndian.Uint64(data[i][:8])
			lo := binary.BigEndian.Uint64(data[i][8:])
			switch {
			case hi < minHi:
				minHi, minIndex = hi, i
			case hi == minHi:
				minLo := binary.BigEndian.Uint64(data[minIndex][8:])
				if lo < minLo {
					minHi, minIndex = hi, i
				}
			}
			switch {
			case hi > maxHi:
				maxHi, maxIndex = hi, i
			case hi == maxHi:
				maxLo := binary.BigEndian.Uint64(data[maxIndex][8:])
				if lo > maxLo {
					maxHi, maxIndex = hi, i
				}
			}
		}
		min = data[minIndex][:]
		max = data[maxIndex][:]
	}
	return min, max
}
