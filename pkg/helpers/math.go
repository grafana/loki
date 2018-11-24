package helpers

// MinUint32 return the min of a and b.
func MinUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
