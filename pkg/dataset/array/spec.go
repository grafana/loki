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

	// SpecBinary holds raw binary data without any compression.
	//
	// The first child Spec determines how to encode offset information, where
	// the half-open range (offsets[i], offsets[i+1]] specifies where the
	// offsets where element i.
	//
	// If the data type is nullable, then the last child Spec describes how to
	// encode validity data.
	SpecBinary struct {
		// Spec for how to encode offset information, where the half-open range
		// (offsets[i], offsets[i+1]] specifies where the offsets where element
		// i.
		Offsets Spec

		// Spec for how to encode validity data. Must only be set for nullable
		// data types.
		Validity Spec
	}

	// SpecBitpacked encodes unsigned integer values using block-based
	// bitpacking. Values are grouped into blocks of BlockSize rows, and each
	// block is packed using the minimum bit width needed for its largest
	// value.
	SpecBitpacked struct {
		// Number of rows per block.
		BlockSize int

		// Spec for encoding per-block bit widths (child array).
		Widths Spec

		// Spec for how to encode validity data. Must only be set for nullable
		// data types.
		Validity Spec
	}

	// SpecZstd encodes variable-length data with zstd compression on the
	// data buffer. Structure mirrors [SpecBinary].
	SpecZstd struct {
		// Spec for how to encode offset information, where the half-open range
		// (offsets[i], offsets[i+1]] specifies where the offsets where element
		// i.
		Offsets Spec

		// Spec for how to encode validity data. Must only be set for nullable
		// data types.
		Validity Spec
	}

	// SpecZigZag encodes signed integer values by mapping them to unsigned
	// integers using zigzag encoding. The unsigned data is then encoded
	// according to the Data spec. Nullability is passed through to the
	// Data child.
	SpecZigZag struct {
		// Spec for how to encode the zigzag-encoded unsigned data.
		Data Spec
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

// Kind returns [EncodingKindBinary].
func (spec *SpecBinary) Kind() EncodingKind {
	return EncodingKindBinary
}

// Kind returns [EncodingKindBitpacked].
func (spec *SpecBitpacked) Kind() EncodingKind {
	return EncodingKindBitpacked
}

// Kind returns [EncodingKindZstd].
func (spec *SpecZstd) Kind() EncodingKind {
	return EncodingKindZstd
}

// Kind returns [EncodingKindZigZag].
func (spec *SpecZigZag) Kind() EncodingKind {
	return EncodingKindZigZag
}

//
// Sealed marker implementations.
//

func (spec *SpecBool) isSpec()      {}
func (spec *SpecPlain) isSpec()     {}
func (spec *SpecBinary) isSpec()    {}
func (spec *SpecBitpacked) isSpec() {}
func (spec *SpecZstd) isSpec()      {}
func (spec *SpecZigZag) isSpec()    {}
