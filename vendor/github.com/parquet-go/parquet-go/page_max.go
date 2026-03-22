package parquet

import (
	"bytes"
)

func maxFixedLenByteArray(data []byte, size int) (max []byte) {
	if len(data) > 0 {
		max = data[:size]

		for i, j := size, 2*size; j <= len(data); {
			item := data[i:j]

			if bytes.Compare(item, max) > 0 {
				max = item
			}

			i += size
			j += size
		}
	}
	return max
}
