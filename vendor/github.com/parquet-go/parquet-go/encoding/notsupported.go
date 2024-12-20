package encoding

import (
	"errors"
	"fmt"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/format"
)

var (
	// ErrNotSupported is an error returned when the underlying encoding does
	// not support the type of values being encoded or decoded.
	//
	// This error may be wrapped with type information, applications must use
	// errors.Is rather than equality comparisons to test the error values
	// returned by encoders and decoders.
	ErrNotSupported = errors.New("encoding not supported")

	// ErrInvalidArgument is an error returned one or more arguments passed to
	// the encoding functions are incorrect.
	//
	// As with ErrNotSupported, this error may be wrapped with specific
	// information about the problem and applications are expected to use
	// errors.Is for comparisons.
	ErrInvalidArgument = errors.New("invalid argument")
)

// Error constructs an error which wraps err and indicates that it originated
// from the given encoding.
func Error(e Encoding, err error) error {
	return fmt.Errorf("%s: %w", e, err)
}

// Errorf is like Error but constructs the error message from the given format
// and arguments.
func Errorf(e Encoding, msg string, args ...interface{}) error {
	return Error(e, fmt.Errorf(msg, args...))
}

// ErrEncodeInvalidInputSize constructs an error indicating that encoding failed
// due to the size of the input.
func ErrEncodeInvalidInputSize(e Encoding, typ string, size int) error {
	return errInvalidInputSize(e, "encode", typ, size)
}

// ErrDecodeInvalidInputSize constructs an error indicating that decoding failed
// due to the size of the input.
func ErrDecodeInvalidInputSize(e Encoding, typ string, size int) error {
	return errInvalidInputSize(e, "decode", typ, size)
}

func errInvalidInputSize(e Encoding, op, typ string, size int) error {
	return Errorf(e, "cannot %s %s from input of size %d: %w", op, typ, size, ErrInvalidArgument)
}

// CanEncodeInt8 reports whether e can encode LEVELS values.
func CanEncodeLevels(e Encoding) bool {
	_, err := e.EncodeLevels(nil, nil)
	return !errors.Is(err, ErrNotSupported)
}

// CanEncodeBoolean reports whether e can encode BOOLEAN values.
func CanEncodeBoolean(e Encoding) bool {
	_, err := e.EncodeBoolean(nil, nil)
	return !errors.Is(err, ErrNotSupported)
}

// CanEncodeInt32 reports whether e can encode INT32 values.
func CanEncodeInt32(e Encoding) bool {
	_, err := e.EncodeInt32(nil, nil)
	return !errors.Is(err, ErrNotSupported)
}

// CanEncodeInt64 reports whether e can encode INT64 values.
func CanEncodeInt64(e Encoding) bool {
	_, err := e.EncodeInt64(nil, nil)
	return !errors.Is(err, ErrNotSupported)
}

// CanEncodeInt96 reports whether e can encode INT96 values.
func CanEncodeInt96(e Encoding) bool {
	_, err := e.EncodeInt96(nil, nil)
	return !errors.Is(err, ErrNotSupported)
}

// CanEncodeFloat reports whether e can encode FLOAT values.
func CanEncodeFloat(e Encoding) bool {
	_, err := e.EncodeFloat(nil, nil)
	return !errors.Is(err, ErrNotSupported)
}

// CanEncodeDouble reports whether e can encode DOUBLE values.
func CanEncodeDouble(e Encoding) bool {
	_, err := e.EncodeDouble(nil, nil)
	return !errors.Is(err, ErrNotSupported)
}

// CanEncodeByteArray reports whether e can encode BYTE_ARRAY values.
func CanEncodeByteArray(e Encoding) bool {
	_, err := e.EncodeByteArray(nil, nil, zeroOffsets[:])
	return !errors.Is(err, ErrNotSupported)
}

// CanEncodeFixedLenByteArray reports whether e can encode
// FIXED_LEN_BYTE_ARRAY values.
func CanEncodeFixedLenByteArray(e Encoding) bool {
	_, err := e.EncodeFixedLenByteArray(nil, nil, 1)
	return !errors.Is(err, ErrNotSupported)
}

var zeroOffsets [1]uint32

// NotSupported is a type satisfying the Encoding interface which does not
// support encoding nor decoding any value types.
type NotSupported struct {
}

func (NotSupported) String() string {
	return "NOT_SUPPORTED"
}

func (NotSupported) Encoding() format.Encoding {
	return -1
}

func (NotSupported) EncodeLevels(dst []byte, src []uint8) ([]byte, error) {
	return dst[:0], errNotSupported("LEVELS")
}

func (NotSupported) EncodeBoolean(dst []byte, src []byte) ([]byte, error) {
	return dst[:0], errNotSupported("BOOLEAN")
}

func (NotSupported) EncodeInt32(dst []byte, src []int32) ([]byte, error) {
	return dst[:0], errNotSupported("INT32")
}

func (NotSupported) EncodeInt64(dst []byte, src []int64) ([]byte, error) {
	return dst[:0], errNotSupported("INT64")
}

func (NotSupported) EncodeInt96(dst []byte, src []deprecated.Int96) ([]byte, error) {
	return dst[:0], errNotSupported("INT96")
}

func (NotSupported) EncodeFloat(dst []byte, src []float32) ([]byte, error) {
	return dst[:0], errNotSupported("FLOAT")
}

func (NotSupported) EncodeDouble(dst []byte, src []float64) ([]byte, error) {
	return dst[:0], errNotSupported("DOUBLE")
}

func (NotSupported) EncodeByteArray(dst []byte, src []byte, offsets []uint32) ([]byte, error) {
	return dst[:0], errNotSupported("BYTE_ARRAY")
}

func (NotSupported) EncodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error) {
	return dst[:0], errNotSupported("FIXED_LEN_BYTE_ARRAY")
}

func (NotSupported) DecodeLevels(dst []uint8, src []byte) ([]uint8, error) {
	return dst, errNotSupported("LEVELS")
}

func (NotSupported) DecodeBoolean(dst []byte, src []byte) ([]byte, error) {
	return dst, errNotSupported("BOOLEAN")
}

func (NotSupported) DecodeInt32(dst []int32, src []byte) ([]int32, error) {
	return dst, errNotSupported("INT32")
}

func (NotSupported) DecodeInt64(dst []int64, src []byte) ([]int64, error) {
	return dst, errNotSupported("INT64")
}

func (NotSupported) DecodeInt96(dst []deprecated.Int96, src []byte) ([]deprecated.Int96, error) {
	return dst, errNotSupported("INT96")
}

func (NotSupported) DecodeFloat(dst []float32, src []byte) ([]float32, error) {
	return dst, errNotSupported("FLOAT")
}

func (NotSupported) DecodeDouble(dst []float64, src []byte) ([]float64, error) {
	return dst, errNotSupported("DOUBLE")
}

func (NotSupported) DecodeByteArray(dst []byte, src []byte, offsets []uint32) ([]byte, []uint32, error) {
	return dst, offsets, errNotSupported("BYTE_ARRAY")
}

func (NotSupported) DecodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error) {
	return dst, errNotSupported("FIXED_LEN_BYTE_ARRAY")
}

func (NotSupported) EstimateDecodeByteArraySize(src []byte) int {
	return 0
}

func (NotSupported) CanDecodeInPlace() bool {
	return false
}

func errNotSupported(typ string) error {
	return fmt.Errorf("%w for type %s", ErrNotSupported, typ)
}

var (
	_ Encoding = NotSupported{}
)
