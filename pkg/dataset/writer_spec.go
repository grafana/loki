package dataset

import (
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
)

// Spec holds the specification for how to build a dataset with a set of static
// and dynamic fields.
//
// The names of fields across a Spec must be unique.
type Spec struct {
	Fields []FieldSpec
}

type (
	// FieldSpec represents one field of a struct.
	FieldSpec interface {
		isFieldSpec() // Sealed interface.

		FieldName() string
	}

	// A StaticFieldSpec specifies a field with a known type.
	StaticFieldSpec struct {
		Name string      // Field name.
		Spec layout.Spec // Field encoding specification.
		Type types.Type  // Field type.
	}

	// A DynamicFieldSpec specifies a struct field whose full set of keys are not
	// yet known at spec construction. Common use cases are for stream labels or
	// structured metadata.
	//
	// Keys on the dynamic struct type are discovered as callers append values to
	// the field.
	//
	// All keys for a dynamic field are nullable. If an append does not provide a
	// value for a previously discovered key, it is backfilled with null values.
	//
	// When a dataset is flushed, the dynamic field is resolved to the full struct
	// type with all discovered keys.
	DynamicFieldSpec struct {
		// Name holds the name of the dynamic field.
		Name string

		// GetSpec returns the layout spec for a newly discovered (key, typ) pair.
		// It is called once per discovered pair and must not return nil.
		GetSpec func(key string, typ types.Type) layout.Spec
	}
)

// FieldName returns the name of the field.
func (s *StaticFieldSpec) FieldName() string { return s.Name }

// FieldName returns the name of the dynamic field.
func (s *DynamicFieldSpec) FieldName() string { return s.Name }

func (s *StaticFieldSpec) isFieldSpec()  {}
func (s *DynamicFieldSpec) isFieldSpec() {}
