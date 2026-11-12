package errors

import (
	"errors"
	"reflect"
)

// SemanticError is an error of encoding a value of GoType by the semantics of encoding/json/v2, which the encoder
// returns with the output written before it: the v2 json package reports it as its SemanticError, at the place of
// that output.
type SemanticError struct {
	GoType reflect.Type
	Err    error
}

func (e *SemanticError) Error() string {
	s := "json: cannot marshal"
	if e.GoType != nil {
		s += " from Go " + e.GoType.String()
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

func (e *SemanticError) Unwrap() error { return e.Err }

// ErrCycle reports a value which is a value of itself, which JSON can't represent.
var ErrCycle = errors.New("encountered a cycle")

// The errors of the JSON text, which jsontext reports by these very errors.
var (
	ErrDuplicateName = errors.New("duplicate object member name")
	ErrNonStringName = errors.New("object member name must be a string")
	ErrMaxDepth      = errors.New("exceeded max depth")
)

// The errors of a member of an object which is unmarshaled into a struct, which the v2 json package reports in its
// SemanticError, by these very errors.
var (
	ErrUnknownName   = errors.New("unknown object member name")
	ErrAmbiguousName = errors.New("ambiguous object member name")
)

// TextError is an error of the JSON text which the encoder of the semantics of encoding/json/v2 would write, which
// it returns with the output written before it: the v2 json package reports it as jsontext.SyntacticError at the
// place of that output. Err is ErrInvalidUTF8, ErrDuplicateName or ErrNonStringName.
type TextError struct {
	Err error
	// Name is the duplicate name, of ErrDuplicateName, which the pointer of the error points to.
	Name string
	// GoType is the type of the value which made the error, if the v2 json package reports it in a SemanticError,
	// as it does for a name which is not a string.
	GoType reflect.Type
	// Out is the output where the error is, if it is not the output which the encoder returns with it: a name of
	// an object which was written before.
	Out []byte
	// AtDelim is whether the offset of the error is the one of the comma which ends Out, before the name: the
	// place where encoding/json/v2 reports a name of an embedded fallback.
	AtDelim bool
}

func (e *TextError) Error() string { return "jsontext: " + e.Err.Error() }

func (e *TextError) Unwrap() error { return e.Err }

// OutputError is an error of the output Out, which the encoder wrote in a context of its own ( a value written by a
// default representation, or the value of an embedded fallback ): the v2 json package reports Err at the place of
// Out instead of the output which the encoder returns with it.
type OutputError struct {
	Out []byte
	Err error
}

func (e *OutputError) Error() string { return e.Err.Error() }

func (e *OutputError) Unwrap() error { return e.Err }

// MethodError is an error which a method or a function of marshaling returned, for the semantics of
// encoding/json/v2, which the encoder returns with the output written before the call: the v2 json package
// reports it at the place of that output, for the type of the value. How it reports an error which is itself one
// of its SemanticErrors depends on the method ( see MethodKind ).
type MethodError struct {
	GoType reflect.Type
	Err    error
	Kind   MethodKind
	// Out is the output where the error is, of MethodJSONTo, which wrote to the encoder: the output before the
	// call if it is nil. Where is where its pointer points: the next value (+1), the last value (-1), or the
	// level the method stopped in (0).
	Out   []byte
	Where int8
}

// MethodKind is the method or the function which returned a MethodError, which encoding/json names in a
// MarshalerError by its String. The place of a SemanticError which it returns, for encoding/json/v2, is within the
// value it returns, after the place of the call, for MarshalJSON and a function of MarshalFunc; the error is reported
// as it is for MarshalText and AppendText; and it is given the place of the output where it stopped, if it has none,
// for MarshalJSONTo and a function of MarshalToFunc, which write to the encoder.
type MethodKind uint8

const (
	MethodMarshalJSON MethodKind = iota
	MethodMarshalFunc
	MethodMarshalText
	MethodAppendText
	MethodMarshalJSONTo
	MethodMarshalToFunc
)

// methodNames are the names of the kinds of MethodKind, as encoding/json names them in a MarshalerError.
var methodNames = [...]string{
	MethodMarshalJSON:   "MarshalJSON",
	MethodMarshalFunc:   "MarshalFunc",
	MethodMarshalText:   "MarshalText",
	MethodAppendText:    "AppendText",
	MethodMarshalJSONTo: "MarshalJSONTo",
	MethodMarshalToFunc: "MarshalToFunc",
}

func (k MethodKind) String() string { return methodNames[k] }

func (e *MethodError) Error() string {
	return (&SemanticError{GoType: e.GoType, Err: e.Err}).Error()
}

func (e *MethodError) Unwrap() error { return e.Err }

// UnmarshalError is an error of decoding the value at Start of the input into a value of GoType by the semantics of
// encoding/json/v2, which the decoder stops at: the v2 json package reports it as its SemanticError, at the place
// of the value, unless the input has a syntax error before it.
type UnmarshalError struct {
	GoType reflect.Type
	Err    error
	// Start is the position of the value in the input, or of the bracket which ends an array for an error of the
	// length of the array. End is the position after the value if HasValue is set: the error reports the value, a
	// number or a string.
	Start, End int64
	HasValue   bool
	// NoKind is set for an error of the Go type, before the value is read: it reports no kind of JSON value.
	NoKind bool
	// Method is the method of unmarshaling, or the time type, which the value was read whole for and which returned
	// Err, which the v2 json package wraps as encoding/json/v2 wraps it, or whose read of the value failed with Err.
	Method UnmarshalMethod
}

// UnmarshalMethod is the method of unmarshaling of an UnmarshalError.
type UnmarshalMethod uint8

const (
	// UnmarshalByKind is an error of the value of its kind, which no method read.
	UnmarshalByKind UnmarshalMethod = iota
	// UnmarshalJSONMethod is an error of UnmarshalJSON or of a function of UnmarshalFunc: a SemanticError it returns
	// is within the value, after the place of the value. With ReportErrorsWithLegacySemantics, it is also the error of
	// UnmarshalJSONFrom or of a function of UnmarshalFromFunc, which encoding/json returns as it is.
	UnmarshalJSONMethod
	// UnmarshalTextMethod is an error of UnmarshalText or of the time.Time of a string: a SemanticError or a
	// SyntacticError it returns is reported as it is.
	UnmarshalTextMethod
	// UnmarshalWholeValue is the error of the text of a value which a stream read whole for its decoder, before it
	// decoded it, as encoding/json/v2 reads a value whole for a method, a function or a type of a string, a number or
	// bytes.
	UnmarshalWholeValue
)

func (e *UnmarshalError) Error() string {
	s := "json: cannot unmarshal"
	if e.GoType != nil {
		s += " into Go " + e.GoType.String()
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

func (e *UnmarshalError) Unwrap() error { return e.Err }
