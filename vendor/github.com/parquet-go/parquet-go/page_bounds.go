package parquet

import "bytes"

func boundsFixedLenByteArray(data []byte, size int) (min, max []byte) {
	if len(data) > 0 {
		min = data[:size]
		max = data[:size]

		for i, j := size, 2*size; j <= len(data); {
			item := data[i:j]

			if bytes.Compare(item, min) < 0 {
				min = item
			}
			if bytes.Compare(item, max) > 0 {
				max = item
			}

			i += size
			j += size
		}
	}
	return min, max
}
