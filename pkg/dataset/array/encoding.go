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
)

// Kind returns [EncodingKindBool].
func (enc *EncodingBool) Kind() EncodingKind {
	return EncodingKindBool
}

//
// Sealed marker implementations.
//

func (enc *EncodingBool) isEncoding() {}
