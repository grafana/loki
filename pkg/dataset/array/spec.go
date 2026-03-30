package array

// Spec describes how to encode an [Array]'s data.
//
// [Encoding] is the counterpart to Spec which is used for reading the
// resulting Array.
//
// While [EncodingKind] categorizes the encoding, the invidiual Encoding
// implementations can hold additionally encoding-specific metadata that is
// used for encoding and decoding.
type Spec interface {
	isSpec() // Sealed interface.

	// Kind returns the encoding kind of this Spec.
	Kind() EncodingKind
}

// Spec definitions.
type (
	// SpecBool encodes bit-packed boolean values.
	SpecBool struct {
		// Spec for how to encode validity data. Must only be set for nullable
		// data types.
		Validity Spec
	}

	// SpecPlain encodes fixed-width values without any compression.
	SpecPlain struct {
		// Spec for how to encode validity data. Must only be set for nullable
		// data types.
		Validity Spec
	}
)

// Kind returns [EncodingKindBool].
func (spec *SpecBool) Kind() EncodingKind {
	return EncodingKindBool
}

// Kind returns [EncodingKindPlain].
func (spec *SpecPlain) Kind() EncodingKind {
	return EncodingKindPlain
}

//
// Sealed marker implementations.
//

func (spec *SpecBool) isSpec()  {}
func (spec *SpecPlain) isSpec() {}
