package array

// Encoding describes how an [Array]'s data is encoded and how it should be
// read.
//
// [Spec] is the counterpart to Encoding which is used for building new Arrays.
//
// While [EncodingKind] categorizes the encoding, the invidiual Encoding
// implementations can hold additionally encoding-specific metadata that is
// required for proper decoding.
type Encoding interface {
	isEncoding() // Sealed interface.

	// Kind returns the encoding kind of this Encoding.
	Kind() EncodingKind
}

// Encoding definitions.
type (
	// EncodingBool holds bit-packed boolean values.
	//
	// If the data type is nullable, then the first child Array holds validity
	// data.
	EncodingBool struct{}

	// EncodingPlain holds fixed-width values without any compression.
	//
	// If the data type is nullable, then the first child Array holds validity
	// data.
	EncodingPlain struct{}

	// EncodingBinary holds raw binary data without any compression. The first
	// child Array holds N+1 offsets into the binary data, where N is the number
	// of elements. The half-open range (offsets[i], offsets[i+1]] specifies the
	// data for element i.
	//
	// If the data type is nullable, then the last child Array holds validity
	// data.
	EncodingBinary struct{}

	// EncodingBitpacked holds bitpacked unsigned integer values split into
	// fixed-size blocks. Each block is independently packed using the minimum
	// number of bits needed for the largest value in that block.
	//
	// The first child Array holds per-block bit widths. If the data type is
	// nullable, then the last child Array holds validity data.
	EncodingBitpacked struct {
		BlockSize int // Number of rows per block. The last block may have fewer rows.
	}

	// EncodingZstd holds zstd-compressed binary data. The structure mirrors
	// [EncodingBinary]: the first child Array holds N+1 offsets, and if
	// nullable, the last child Array holds validity data. The data buffer is
	// zstd-compressed.
	EncodingZstd struct {
		// UncompressedSize is the byte length of the data buffer before
		// compression. The reader uses this to pre-allocate the decode buffer.
		UncompressedSize int
	}
)

// Kind returns [EncodingKindBool].
func (enc *EncodingBool) Kind() EncodingKind {
	return EncodingKindBool
}

// Kind returns [EncodingKindPlain].
func (enc *EncodingPlain) Kind() EncodingKind {
	return EncodingKindPlain
}

// Kind returns [EncodingKindBinary].
func (enc *EncodingBinary) Kind() EncodingKind {
	return EncodingKindBinary
}

// Kind returns [EncodingKindBitpacked].
func (enc *EncodingBitpacked) Kind() EncodingKind {
	return EncodingKindBitpacked
}

// Kind returns [EncodingKindZstd].
func (enc *EncodingZstd) Kind() EncodingKind {
	return EncodingKindZstd
}

//
// Sealed marker implementations.
//

func (enc *EncodingBool) isEncoding()      {}
func (enc *EncodingPlain) isEncoding()     {}
func (enc *EncodingBinary) isEncoding()    {}
func (enc *EncodingBitpacked) isEncoding() {}
func (enc *EncodingZstd) isEncoding()      {}
