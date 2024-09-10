package v1

import (
	"fmt"
	"io"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

type Version byte

func (v Version) String() string {
	return fmt.Sprintf("v%d", v)
}

const (
	magicNumber = uint32(0xCA7CAFE5)

	// Add new versions below
	V1 Version = iota
	// V2 supports single series blooms encoded over multiple pages
	// to accommodate larger single series
	V2
	// V2 indicated schema for indexed structured metadata
	V3

	CurrentSchemaVersion = V3
)

var (
	SupportedVersions = []Version{V3}

	ErrInvalidSchemaVersion     = errors.New("invalid schema version")
	ErrUnsupportedSchemaVersion = errors.New("unsupported schema version")
)

type Schema struct {
	version                Version
	encoding               chunkenc.Encoding
	nGramLength, nGramSkip uint64
}

func NewSchema() Schema {
	return Schema{
		version:     CurrentSchemaVersion,
		encoding:    chunkenc.EncNone,
		nGramLength: 0,
		nGramSkip:   0,
	}
}

func (s Schema) String() string {
	return fmt.Sprintf("%s,encoding=%s,ngram=%d,skip=%d", s.version, s.encoding, s.nGramLength, s.nGramSkip)
}

func (s Schema) Compatible(other Schema) bool {
	return s == other
}

func (s Schema) Version() Version {
	return s.version
}

func (s Schema) NGramLen() int {
	return int(s.nGramLength)
}

func (s Schema) NGramSkip() int {
	return int(s.nGramSkip)
}

// byte length
func (s Schema) Len() int {
	// magic number + version + encoding + ngram length + ngram skip
	return 4 + 1 + 1 + 8 + 8
}

func (s *Schema) DecompressorPool() chunkenc.ReaderPool {
	return chunkenc.GetReaderPool(s.encoding)
}

func (s *Schema) CompressorPool() chunkenc.WriterPool {
	return chunkenc.GetWriterPool(s.encoding)
}

func (s *Schema) Encode(enc *encoding.Encbuf) {
	enc.Reset()
	enc.PutBE32(magicNumber)
	enc.PutByte(byte(s.version))
	enc.PutByte(byte(s.encoding))
	enc.PutBE64(s.nGramLength)
	enc.PutBE64(s.nGramSkip)

}

func (s *Schema) DecodeFrom(r io.ReadSeeker) error {
	// TODO(owen-d): improve allocations
	schemaBytes := make([]byte, s.Len())
	_, err := io.ReadFull(r, schemaBytes)
	if err != nil {
		return errors.Wrap(err, "reading schema")
	}

	dec := encoding.DecWith(schemaBytes)
	return s.Decode(&dec)
}

func (s *Schema) Decode(dec *encoding.Decbuf) error {
	number := dec.Be32()
	if number != magicNumber {
		return errors.Errorf("invalid magic number. expected %x, got  %x", magicNumber, number)
	}
	s.version = Version(dec.Byte())
	if s.version != V3 {
		return errors.Errorf("invalid version. expected %d, got %d", 3, s.version)
	}

	s.encoding = chunkenc.Encoding(dec.Byte())
	if _, err := chunkenc.ParseEncoding(s.encoding.String()); err != nil {
		return errors.Wrap(err, "parsing encoding")
	}

	s.nGramLength = dec.Be64()
	s.nGramSkip = dec.Be64()

	return dec.Err()
}
