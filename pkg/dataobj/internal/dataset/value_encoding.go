package dataset

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/memory"
)

// A valueEncoder encodes sequences of [Value], writing them to an underlying
// [streamio.Writer]. Implementations of encoding types must call
// registerValueEncoding to register themselves.
type valueEncoder interface {
	// PhysicalType returns the type of values supported by the valueEncoder.
	PhysicalType() datasetmd.PhysicalType

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

// A legacyValueDecoder decodes sequences of [Value] from an underlying
// byte slice. Implementations of encoding types must call registerValueEncoding
// to register themselves.
type legacyValueDecoder interface {
	// PhysicalType returns the type of values supported by the decoder.
	PhysicalType() datasetmd.PhysicalType

	// EncodingType returns the encoding type used by the decoder.
	EncodingType() datasetmd.EncodingType

	// Decode decodes up to len(s) values, storing the results into s. The
	// number of decoded values is returned, followed by an error (if any).
	// At the end of the stream, Decode returns 0, [io.EOF].
	Decode(s []Value) (int, error)

	// Reset discards any state and resets the decoder to read from data.
	// This permits reusing a decoder rather than allocating a new one.
	Reset(data []byte)
}

// A valueDecoder reads values from an underlying byte slice. Implementations of
// encoding types must call registerValueEncoding to register themselves.
//
// valueDecoder supersedes [legacyValueDecoder].
type valueDecoder interface {
	// PhysicalType returns the type of values supported by the decoder.
	PhysicalType() datasetmd.PhysicalType

	// EncodingType returns the encoding type used by the decoder.
	EncodingType() datasetmd.EncodingType

	// Decode up to count values using the provided allocator. An opaque return
	// variable represents the decoded values. Callers are responsible for
	// understanding which concrete type the return value is based on the
	// encoding type.
	//
	// TODO(rfratto): Replace the any return type with a more general "array"
	// type.
	Decode(alloc *memory.Allocator, count int) (any, error)

	// Reset discards any state and resets the decoder to read from data.
	// This permits reusing a decoder rather than allocating a new one.
	Reset(data []byte)
}

// registry stores known value encoders and decoders. We use a global variable
// to track implementations to allow encoding implementations to be
// self-contained in a single file.
var registry = map[registryKey]registryEntry{}

type (
	registryKey struct {
		Physical datasetmd.PhysicalType
		Encoding datasetmd.EncodingType
	}

	registryEntry struct {
		NewEncoder       func(streamio.Writer) valueEncoder
		NewDecoder       func([]byte) valueDecoder
		NewLegacyDecoder func([]byte) legacyValueDecoder
	}
)

// registerValueEncoding registers an encoder and decoder for a specified
// physicalType and encodingType tuple. If another encoding has been registered
// for the same tuple, registerValueEncoding panics.
//
// registerValueEncoding should be called in an init method of files
// implementing encodings.
func registerValueEncoding(
	physicalType datasetmd.PhysicalType,
	encodingType datasetmd.EncodingType,
	entry registryEntry,
) {
	key := registryKey{
		Physical: physicalType,
		Encoding: encodingType,
	}
	if _, exist := registry[key]; exist {
		panic(fmt.Sprintf("dataset: registerValueEncoding already called for %s/%s", physicalType, encodingType))
	}

	registry[key] = entry
}

// newValueEncoder creates a new valueEncoder for the specified physicalType and
// encodingType. If no encoding is registered for the specified combination of
// physicalType and encodingType, newValueEncoder returns nil and false.
func newValueEncoder(physicalType datasetmd.PhysicalType, encodingType datasetmd.EncodingType, w streamio.Writer) (valueEncoder, bool) {
	key := registryKey{
		Physical: physicalType,
		Encoding: encodingType,
	}
	entry, exist := registry[key]
	if !exist {
		return nil, false
	}
	return entry.NewEncoder(w), true
}

// newValueDecoder creates a new decoder for the specified physicalType and
// encodingType. If no encoding is registered for the specified combination of
// physicalType and encodingType, newValueDecoder returns nil and false.
func newValueDecoder(physicalType datasetmd.PhysicalType, encodingType datasetmd.EncodingType, data []byte) (legacyValueDecoder, bool) {
	key := registryKey{
		Physical: physicalType,
		Encoding: encodingType,
	}
	entry, exist := registry[key]
	if !exist {
		return nil, false
	}

	switch {
	case entry.NewDecoder != nil:
		dec := &valueDecoderAdapter{
			// TODO(rfratto): Pass through an allocator to avoid the overhead here.
			Alloc: new(memory.Allocator),
			Inner: entry.NewDecoder(data),
		}
		return dec, true

	case entry.NewLegacyDecoder != nil:
		return entry.NewLegacyDecoder(data), true

	default:
		return nil, false
	}
}
