package util

func MinUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
