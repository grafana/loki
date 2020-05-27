package common

// UnsafeUTF8DecimalCodePointsToInt converts a slice containing
// a series of UTF-8 decimal code points into their integer rapresentation.
//
// It assumes input code points are in the range 48-57.
// Returns a pointer since an empty slice is equal to nil and not to the zero value of the codomain (ie., `int`).
func UnsafeUTF8DecimalCodePointsToInt(chars []uint8) int {
	out := 0
	ord := 1
	for i := len(chars) - 1; i >= 0; i-- {
		curchar := int(chars[i])
		out += (curchar - '0') * ord
		ord *= 10
	}
	return out
}

// RemoveBytes removes byte at given positions from data byte slice, starting from the given offset.
func RemoveBytes(data []byte, positions []int, offset int) []byte {
	// We need a copy here to not modify original data
	cp := append([]byte(nil), data...)
	for i, pos := range positions {
		at := pos - i - offset
		cp = append(cp[:at], cp[(at+1):]...)
	}
	return cp
}

// EscapeBytes adds a backslash to \, ], " characters.
func EscapeBytes(value string) string {
	res := ""
	for i, c := range value {
		// todo(leodido): generalize byte codes (the function should ideally accept a byte slice containing byte codes to escape)
		if c == 92 || c == 93 || c == 34 {
			res += `\`
		}
		res += string(value[i])
	}

	return res
}

// InBetween tells whether value is into [min, max] range.
func InBetween(val, min, max int) bool {
	return val >= min && val <= max
}

// ValidPriority checks whether the given value is in the priority range [0, 191].
func ValidPriority(priority uint8) bool {
	return InBetween(int(priority), 0, 191)
}

// ValidVersion checks whether the given value is in the version range [1, 999].
func ValidVersion(version uint16) bool {
	return InBetween(int(version), 1, 999)
}
