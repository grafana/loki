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

// utf8Tables are the tables of the validation of UTF-8 by SIMD, by the lookups of "Validating UTF-8 In Less Than
// One Instruction Per Byte" ( John Keiser, Daniel Lemire, 2020 ): the errors of two bytes in a row are found by
// three lookups of 16 bytes, by the high and the low nibble of the first byte and the high nibble of the second
// one; each bit is a kind of error, which the three agree on only if the two bytes are that error. What they miss,
// that the third or the fourth byte of a character is not a continuation byte or the reverse, is found from the
// bytes two and three before.
type utf8Tables struct {
	Byte1High [16]byte
	Byte1Low  [16]byte
	Byte2High [16]byte
}

// The kinds of the errors of two bytes in a row ( see utf8Tables ).
const (
	utf8TooShort     = 1 << 0 // 11______ 0_______, 11______ 11______: a leading byte without its continuation
	utf8TooLong      = 1 << 1 // 0_______ 10______: a continuation byte without its leading byte
	utf8Overlong3    = 1 << 2 // 11100000 100_____
	utf8TooLarge     = 1 << 3 // 11110100 1001____, 11110100 101_____, 11110101 1001____, ...: above U+10FFFF
	utf8Surrogate    = 1 << 4 // 11101101 101_____: U+D800 to U+DFFF
	utf8Overlong2    = 1 << 5 // 1100000_ 10______
	utf8TooLarge1000 = 1 << 6 // 11110101 1000____, ...: above U+10FFFF
	utf8Overlong4    = 1 << 6 // 11110000 1000____
	utf8TwoConts     = 1 << 7 // 10______ 10______: a continuation byte, unless it is the third or the fourth
	utf8Carry        = utf8TooShort | utf8TooLong | utf8TwoConts
)

var utf8ValidationTables = utf8Tables{
	Byte1High: [16]byte{
		// 0_______: ASCII
		utf8TooLong, utf8TooLong, utf8TooLong, utf8TooLong, utf8TooLong, utf8TooLong, utf8TooLong, utf8TooLong,
		// 10______: a continuation byte
		utf8TwoConts, utf8TwoConts, utf8TwoConts, utf8TwoConts,
		// 1100____, 1101____: the leading byte of two bytes
		utf8TooShort | utf8Overlong2,
		utf8TooShort,
		// 1110____: the leading byte of three bytes
		utf8TooShort | utf8Overlong3 | utf8Surrogate,
		// 1111____: the leading byte of four bytes
		utf8TooShort | utf8TooLarge | utf8TooLarge1000 | utf8Overlong4,
	},
	Byte1Low: [16]byte{
		utf8Carry | utf8Overlong3 | utf8Overlong2 | utf8Overlong4, // ____0000
		utf8Carry | utf8Overlong2,                                 // ____0001
		utf8Carry,                                                 // ____0010
		utf8Carry,                                                 // ____0011
		utf8Carry | utf8TooLarge,                                  // ____0100
		utf8Carry | utf8TooLarge | utf8TooLarge1000,               // ____0101
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000 | utf8Surrogate, // ____1101
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
		utf8Carry | utf8TooLarge | utf8TooLarge1000,
	},
	Byte2High: [16]byte{
		// ________ 0_______: ASCII
		utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort,
		// ________ 1000____
		utf8TooLong | utf8Overlong2 | utf8TwoConts | utf8Overlong3 | utf8TooLarge1000 | utf8Overlong4,
		// ________ 1001____
		utf8TooLong | utf8Overlong2 | utf8TwoConts | utf8Overlong3 | utf8TooLarge,
		// ________ 101_____
		utf8TooLong | utf8Overlong2 | utf8TwoConts | utf8Surrogate | utf8TooLarge,
		utf8TooLong | utf8Overlong2 | utf8TwoConts | utf8Surrogate | utf8TooLarge,
		// ________ 11______: a leading byte
		utf8TooShort, utf8TooShort, utf8TooShort, utf8TooShort,
	},
}
