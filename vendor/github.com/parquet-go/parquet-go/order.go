package parquet

import (
	"bytes"

	"github.com/parquet-go/parquet-go/internal/unsafecast"
)

func orderOfBool(data []bool) int {
	switch len(data) {
	case 0, 1:
		return 0
	default:
		k := 0
		i := 0

		if data[0] { // true => false: descending
			k = -1
			i = streakOfTrue(data)
			if i == len(data) {
				k = +1
			} else {
				i += streakOfFalse(data[i:])
			}
		} else { // false => true: ascending
			k = +1
			i = streakOfFalse(data)
			i += streakOfTrue(data[i:])
		}

		if i != len(data) {
			k = 0
		}
		return k
	}
}

func streakOfTrue(data []bool) int {
	if i := bytes.IndexByte(unsafecast.Slice[byte](data), 0); i >= 0 {
		return i
	}
	return len(data)
}

func streakOfFalse(data []bool) int {
	if i := bytes.IndexByte(unsafecast.Slice[byte](data), 1); i >= 0 {
		return i
	}
	return len(data)
}

func orderOfBytes(data [][]byte) int {
	switch len(data) {
	case 0, 1:
		return 0
	}
	data = skipBytesStreak(data)
	if len(data) < 2 {
		return 1
	}
	ordering := bytes.Compare(data[0], data[1])
	switch {
	case ordering < 0:
		if bytesAreInAscendingOrder(data[1:]) {
			return +1
		}
	case ordering > 0:
		if bytesAreInDescendingOrder(data[1:]) {
			return -1
		}
	}
	return 0
}

func skipBytesStreak(data [][]byte) [][]byte {
	for i := 1; i < len(data); i++ {
		if !bytes.Equal(data[i], data[0]) {
			return data[i-1:]
		}
	}
	return data[len(data)-1:]
}

func bytesAreInAscendingOrder(data [][]byte) bool {
	for i := len(data) - 1; i > 0; i-- {
		k := bytes.Compare(data[i-1], data[i])
		if k > 0 {
			return false
		}
	}
	return true
}

func bytesAreInDescendingOrder(data [][]byte) bool {
	for i := len(data) - 1; i > 0; i-- {
		k := bytes.Compare(data[i-1], data[i])
		if k < 0 {
			return false
		}
	}
	return true
}
