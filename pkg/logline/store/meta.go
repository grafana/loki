package store

import (
	"fmt"
	"io"
	"os"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// Meta is the catalog type for a single index in object storage.
type Meta = logproto.Meta

// SetFileInfo records the on-disk facts about a finished index file: its size
// and its xxh3 content hash. The file's read offset is reset to 0 before
// returning so the caller can hand the same handle to PutIndex.
//
// IndexHeader is deliberately not set here. Decoding a header requires
// resolving the file to a format version, which is the job of the layer above
// the store, so the caller assigns IndexHeader itself.
func SetFileInfo(m *Meta, f *os.File) error {
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
