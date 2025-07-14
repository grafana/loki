//go:build purego || !amd64

package delta

func encodeByteArrayLengths(lengths []int32, offsets []uint32) {
	for i := range lengths {
		lengths[i] = int32(offsets[i+1] - offsets[i])
	}
}

func decodeByteArrayLengths(offsets []uint32, lengths []int32) (uint32, int32) {
	lastOffset := uint32(0)

	for i, n := range lengths {
		if n < 0 {
			return lastOffset, n
		}
		offsets[i] = lastOffset
		lastOffset += uint32(n)
	}

	offsets[len(lengths)] = lastOffset
	return lastOffset, 0
}
