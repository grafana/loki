package layout

import "github.com/grafana/loki/v3/pkg/dataset/array"

// Spec describes how to encode data for a [Layout].
//
// While [Kind] categorizes the spec, the invidiual Spec implementations can
// hold additionally encoding-specific metadata that is used for encoding and
// decoding.
type Spec interface {
	isSpec() // Sealed interface.

	// Kind returns the encoding kind of this Spec.
	Kind() Kind
}

// Spec definitions.
type (
	// SpecArray specifies how to encode an individual [array.Array].
	SpecArray struct {
		// Spec for encoding data in the Array.
		Spec array.Spec
	}
)

// Kind returns [KindArray].
func (spec *SpecArray) Kind() Kind {
	return KindArray
}

//
// Sealed marker implementations.
//

func (spec *SpecArray) isSpec() {}
