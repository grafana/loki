package encoder

// nibbleTables are two tables of 16 bytes for the scan of a string by SIMD: a byte b may need an escape if
// Lo[b&0xf] & Hi[b>>4] is not zero. The bytes of a high nibble which need an escape are a set of low
// nibbles; every distinct set gets a bit, which Hi has for the high nibbles of the set and Lo has for the
// low nibbles in the set. Two lookups of a byte per lane replace a compare per character to escape.
type nibbleTables struct {
	Lo [16]byte
	Hi [16]byte
}

// newNibbleTables returns the tables for the bytes which need an escape. It panics if more than 8 distinct
// sets of low nibbles are needed, which the tables of the encoder never need.
func newNibbleTables(need *[256]bool) nibbleTables {
	var t nibbleTables
	bits := map[uint16]byte{} // the set of the low nibbles, as a bit set -> the bit of the set
	next := byte(1)
	for hi := 0; hi < 16; hi++ {
		var set uint16
		for lo := 0; lo < 16; lo++ {
			if need[hi<<4|lo] {
				set |= 1 << lo
			}
		}
		if set == 0 {
			continue
		}
		bit, ok := bits[set]
		if !ok {
			if next == 0 {
				panic("too many sets of the low nibbles for the tables of the scan")
			}
			bit = next
			next <<= 1
			bits[set] = bit
			for lo := 0; lo < 16; lo++ {
				if set&(1<<lo) != 0 {
					t.Lo[lo] |= bit
				}
			}
		}
		t.Hi[hi] = bit
	}
	return t
}
