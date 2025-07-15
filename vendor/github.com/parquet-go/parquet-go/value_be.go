//go:build s390x

package parquet

// On a big endian system, a boolean/byte value, which is in little endian byte
// format, is byte aligned to the 7th byte in a u64 (8 bytes) variable.
// Hence the data will be available at 7th byte when interpreted as a little
// endian byte format. So, in order to access a boolean/byte value out of u64
// variable, we need to add an offset of "7".
//
// In the same way, an int32/uint32/float value, which is in little endian byte
// format, is byte aligned to the 4th byte in a u64 (8 bytes) variable.
// Hence the data will be available at 4th byte when interpreted as a little
// endian byte format. So, in order to access an int32/uint32/float value out of
// u64 variable, we need to add an offset of "4".
const (
	firstByteOffsetOfBooleanValue = 7
	firstByteOffsetOf32BitsValue  = 4
)
