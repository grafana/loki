// Package types describes a set of logical types that can be represented by an
// array or a scalar.
package types

import (
	"strings"
)

// A Type describes the logical type of data.
type Type interface {
	isType() // Sealed method to prevent external implementations.

	// Kind returns the [Kind] of the Type.
	Kind() Kind

	// String returns a human-readable representation of the Type.
	String() string
}

// Type definitions.
type (
	// Null is a niladic type whose only valid value is NULL.
	Null struct{}

	// Uint32 describes unsigned 32-bit integer values.
	Uint32 struct{ Nullable bool }

	// Uint64 describes unsigned 64-bit integer values.
	Uint64 struct{ Nullable bool }

	// Int32 describes signed 32-bit integer values.
	Int32 struct{ Nullable bool }

	// Int64 describes signed 64-bit integer values.
	Int64 struct{ Nullable bool }

	// Bool describes boolean values.
	Bool struct{ Nullable bool }

	// UTF8 describes variable-length UTF-8 string values.
	UTF8 struct{ Nullable bool }

	// Struct describes a composite type with an ordered set of named fields.
	Struct struct {
		// Fields is the ordered list of fields in the struct.
		Fields []StructField

		// Nullable indicates whether the struct value itself can be null. This
		// value does not impact whether an individual field can be null.
		Nullable bool
	}

	// StructField is a named field within a [Struct] type.
	StructField struct {
		// Name is the field name.
		Name string

		// Type is the type of the field's values.
		Type Type
	}

	// List represents a variable-length list element type. Every element in the
	// list shares the same [Type].
	List struct {
		Element  Type
		Nullable bool
	}
)

// Kind returns [KindNull].
func (t *Null) Kind() Kind {
	return KindNull
}

// String returns "null".
func (t *Null) String() string {
	return "null"
}

// Kind returns [KindUint64].
func (t *Uint64) Kind() Kind {
	return KindUint64
}

// String returns "uint64" or "uint64?" if nullable.
func (t *Uint64) String() string {
	return "uint64" + nullable(t.Nullable)
}

// nullable returns "?" if nullable is true, otherwise "".
func nullable(n bool) string {
	if n {
		return "?"
	}
	return ""
}

// Kind returns [KindUint32].
func (t *Uint32) Kind() Kind {
	return KindUint32
}

// String returns "uint32" or "uint32?" if nullable.
func (t *Uint32) String() string {
	return "uint32" + nullable(t.Nullable)
}

// Kind returns [KindInt32].
func (t *Int32) Kind() Kind {
	return KindInt32
}

// String returns "int32" or "int32?" if nullable.
func (t *Int32) String() string {
	return "int32" + nullable(t.Nullable)
}

// Kind returns [KindInt64].
func (t *Int64) Kind() Kind {
	return KindInt64
}

// String returns "int64" or "int64?" if nullable.
func (t *Int64) String() string {
	return "int64" + nullable(t.Nullable)
}

// Kind returns [KindBool].
func (t *Bool) Kind() Kind {
	return KindBool
}

// String returns "bool" or "bool?" if nullable.
func (t *Bool) String() string {
	return "bool" + nullable(t.Nullable)
}

// Kind returns [KindUTF8].
func (t *UTF8) Kind() Kind {
	return KindUTF8
}

// String returns "utf8" or "utf8?" if nullable.
func (t *UTF8) String() string {
	return "utf8" + nullable(t.Nullable)
}

// Kind returns [KindStruct].
func (t *Struct) Kind() Kind {
	return KindStruct
}

// String returns a representation like "struct<name: utf8, age: int32>".
func (t *Struct) String() string {
	var b strings.Builder
	b.WriteString("struct<")
	for i, f := range t.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Name)
		b.WriteString(": ")
		b.WriteString(f.Type.String())
	}
	b.WriteByte('>')
	b.WriteString(nullable(t.Nullable))
	return b.String()
}

// Kind returns [KindList].
func (t *List) Kind() Kind {
	return KindList
}

// String returns a representation like "list<utf8>".
func (t *List) String() string {
	return "list<" + t.Element.String() + ">" + nullable(t.Nullable)
}

// isType marker methods.

func (t *Null) isType()   {}
func (t *Uint32) isType() {}
func (t *Uint64) isType() {}
func (t *Int32) isType()  {}
func (t *Int64) isType()  {}
func (t *Bool) isType()   {}
func (t *UTF8) isType()   {}
func (t *Struct) isType() {}
func (t *List) isType()   {}
