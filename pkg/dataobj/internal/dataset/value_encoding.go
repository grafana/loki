package dataset

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

// A valueEncoder encodes sequences of [Value], writing them to an underlying
// [streamio.Writer]. Implementations of encoding types must call
// registerValueEncoding to register themselves.
type valueEncoder interface {
	// ValueType returns the type of values supported by the valueEncoder.
	ValueType() datasetmd.ValueType

	// EncodingType returns the encoding type used by the valueEncoder.
	EncodingType() datasetmd.EncodingType

	// Encode encodes an individual [Value]. Encode returns an error if encoding
	// fails or if value is an unsupported type.
	Encode(value Value) error

	// Flush encodes any buffered data and immediately writes it to the
	// underlying [streamio.Writer]. Flush returns an error if encoding fails.
	Flush() error

	// Reset discards any state and resets the valueEncoder to write to w. This
	// permits reusing a valueEncoder rather than allocating a new one.
	Reset(w streamio.Writer)
}

// A valueDecoder decodes sequences of [Value] from an underlying
// [streamio.Reader]. Implementations of encoding types must call
// registerValueEncoding to register themselves.
type valueDecoder interface {
	// ValueType returns the type of values supported by the valueDecoder.
	ValueType() datasetmd.ValueType

	// EncodingType returns the encoding type used by the valueDecoder.
	EncodingType() datasetmd.EncodingType

	// Decode decodes up to len(s) values, storing the results into s. The
	// number of decoded values is returned, followed by an error (if any).
	// At the end of the stream, Decode returns 0, [io.EOF].
	Decode(s []Value) (int, error)

	// Reset discards any state and resets the valueDecoder to read from r. This
	// permits reusing a valueDecoder rather than allocating a new one.
	Reset(r streamio.Reader)
}

// registry stores known value encoders and decoders. We use a global variable
// to track implementations to allow encoding implementations to be
// self-contained in a single file.
var registry = map[registryKey]registryEntry{}

type (
	registryKey struct {
		Value    datasetmd.ValueType
		Encoding datasetmd.EncodingType
	}

	registryEntry struct {
		NewEncoder func(streamio.Writer) valueEncoder
		NewDecoder func(streamio.Reader) valueDecoder
	}
)

// registerValueEncoding registers a [valueEncoder] and [valueDecoder] for a
// specified valueType and encodingType tuple. If another encoding has been
// registered for the same tuple, registerValueEncoding panics.
//
// registerValueEncoding should be called in an init method of files
// implementing encodings.
func registerValueEncoding(
	valueType datasetmd.ValueType,
	encodingType datasetmd.EncodingType,
	newEncoder func(streamio.Writer) valueEncoder,
	newDecoder func(streamio.Reader) valueDecoder,
) {
	key := registryKey{
		Value:    valueType,
		Encoding: encodingType,
	}
	if _, exist := registry[key]; exist {
		panic(fmt.Sprintf("dataset: registerValueEncoding already called for %s/%s", valueType, encodingType))
	}

	registry[key] = registryEntry{
		NewEncoder: newEncoder,
		NewDecoder: newDecoder,
	}
}

// newValueEncoder creates a new valueEncoder for the specified valueType and
// encodingType. If no encoding is registered for the specified combination of
// valueType and encodingType, newValueEncoder returns nil and false.
func newValueEncoder(valueType datasetmd.ValueType, encodingType datasetmd.EncodingType, w streamio.Writer) (valueEncoder, bool) {
	key := registryKey{
		Value:    valueType,
		Encoding: encodingType,
	}
	entry, exist := registry[key]
	if !exist {
		return nil, false
	}
	return entry.NewEncoder(w), true
}

// newValueDecoder creates a new valueDecoder for the specified valueType and
// encodingType. If no encoding is registered for the specified combination of
// valueType and encodingType, vewValueDecoder returns nil and false.
func newValueDecoder(valueType datasetmd.ValueType, encodingType datasetmd.EncodingType, r streamio.Reader) (valueDecoder, bool) {
	key := registryKey{
		Value:    valueType,
		Encoding: encodingType,
	}
	entry, exist := registry[key]
	if !exist {
		return nil, false
	}
	return entry.NewDecoder(r), true
}
