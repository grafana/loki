package store

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// Meta holds the identity and metadata for a single index in object storage.
type Meta struct {
	Date string `json:"date"` // partition date, format "YYYY-MM-DD"

	// StorageID is the opaque object key component used in index IDs and
	// object storage paths. Serialized as "id" in meta.json and validated
	// against the path on read. Legacy indexes omit it and use Hash as the
	// storage ID.
	StorageID string `json:"id"`

	// Hash is the xxh3 content hash of the index file, kept in meta.json for
	// integrity/debugging and shared across builder/worker implementations.
	Hash string `json:"hash"`

	Version string `json:"version"`

	// Inclusive time bounds of log lines in the index.
	MinLogTs time.Time `json:"min_log_ts"`
	MaxLogTs time.Time `json:"max_log_ts"`

	// Inclusive time bounds of the Kafka records used to build the index.
	MinRecordTs time.Time `json:"min_rec_ts"`
	MaxRecordTs time.Time `json:"max_rec_ts"`

	// IDs ("date/id") of source indexes merged to produce this index.
	// Nil means leaf index written directly by the builder.
	CompactedFrom []string `json:"compacted_from,omitempty"`

	// Wall-clock time when this index was written (set by PutIndex).
	CreatedAt time.Time `json:"created_at"`

	// Structural summary of the index file, read from its 256-byte header.
	// Always serialized to meta.json; nil is reserved for legacy indexes.
	IndexHeader *format.HeaderInfo `json:"index_header"`

	// Shard identity for this index file. ShardCount=0 means unsharded (legacy).
	// No omitempty: zero values must always be serialized so old and new indexes
	// are indistinguishable at the JSON level.
	ShardCount     int    `json:"shard_count"`
	ShardAlgorithm string `json:"shard_algorithm"`
	ShardValue     int    `json:"shard_value"`

	// DocumentInterval is the time range each document ID covers. Indexes with
	// different document intervals must not be compacted together. Zero means
	// legacy (pre-interval) index.
	DocumentInterval time.Duration `json:"document_interval,omitempty"`

	// Size of the index data file in bytes. Written to meta.json by callers
	// that know the size at upload time. Legacy indexes omit this field;
	// loadMeta falls back to bucket Attributes when SizeBytes is zero.
	SizeBytes int64 `json:"size_bytes,omitempty"`
}

// SetFileInfo records the on-disk facts about a finished index file: its size
// and its xxh3 content hash. The file's read offset is reset to 0 before
// returning so the caller can hand the same handle to PutIndex.
//
// IndexHeader is deliberately not set here. Decoding a header requires
// resolving the file to a format version, which is the job of the layer above
// the store, so the caller assigns IndexHeader itself.
func (m *Meta) SetFileInfo(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat index file: %w", err)
	}

	hash, err := computeIndexHash(f)
	if err != nil {
		return fmt.Errorf("hash index file: %w", err)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek index file: %w", err)
	}

	m.Hash = hash
	m.SizeBytes = fi.Size()
	return nil
}

// Validate checks that all required fields are set. CompactedFrom and CreatedAt
// are excluded — CompactedFrom is only set on merged indexes, CreatedAt by PutIndex.
func (m Meta) Validate() error {
	if m.Date == "" {
		return fmt.Errorf("date must be non-empty")
	}
	if m.Hash == "" {
		return fmt.Errorf("hash must be non-empty")
	}
	if m.objectID() == "" {
		return fmt.Errorf("storage id must be non-empty")
	}
	if m.Version == "" {
		return fmt.Errorf("version must be non-empty")
	}
	if m.MinLogTs.IsZero() {
		return fmt.Errorf("min_log_ts must be set")
	}
	if m.MaxLogTs.IsZero() {
		return fmt.Errorf("max_log_ts must be set")
	}
	if m.MinRecordTs.IsZero() {
		return fmt.Errorf("min_rec_ts must be set")
	}
	if m.MaxRecordTs.IsZero() {
		return fmt.Errorf("max_rec_ts must be set")
	}
	if m.IndexHeader == nil {
		return fmt.Errorf("index_header must be set")
	}
	if m.SizeBytes <= 0 {
		return fmt.Errorf("size_bytes must be positive")
	}
	return nil
}

func (m Meta) objectID() string {
	if m.StorageID != "" {
		return m.StorageID
	}
	return m.Hash
}

// ID returns the unique identifier for this index ("date/id").
func (m Meta) ID() string {
	return m.Date + "/" + m.objectID()
}

// IndexPath returns the object storage path for the index data file.
func (m Meta) IndexPath() string {
	return fmt.Sprintf("%s/%s/index", m.Date, m.objectID())
}

// MetaPath returns the object storage path for the metadata file.
func (m Meta) MetaPath() string {
	return fmt.Sprintf("%s/%s/meta.json", m.Date, m.objectID())
}
