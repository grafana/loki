// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/file_factory.go

package encoding

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/filepool"
)

// FilePoolDecbufFactory creates new file-backed Decbuf instances
// for a specific index-header file on local disk.
type FilePoolDecbufFactory struct {
	files *filepool.FilePool
}

func NewFilePoolDecbufFactory(
	path string,
	maxIdleFileHandles uint,
	metrics *filepool.FilePoolMetrics,
) *FilePoolDecbufFactory {
	return &FilePoolDecbufFactory{
		files: filepool.NewFilePool(
			path,
			maxIdleFileHandles,
			metrics,
		),
	}
}

func (df *FilePoolDecbufFactory) NewDecbufAtChecked(offset int, table *crc32.Table) Decbuf {
	f, err := df.files.Get()
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "open file for decbuf")}
	}

	// If we return early and don't include a BufReader for our Decbuf, we are responsible
	// for putting the file handle back in the pool.
	closeFile := true
	defer func() {
		if closeFile {
			_ = df.files.Put(f)
		}
	}()

	// TODO: A particular index-header only has symbols and posting offsets. We should only need to read
	//  the length of each of those a single time per index-header (DecbufFactory). Should the factory
	//  cache the length? Should the table of contents be passed to the factory?
	lengthBytes := make([]byte, numLenBytes)
	n, err := f.ReadAt(lengthBytes, int64(offset))
	if err != nil {
		return Decbuf{E: err}
	}
	if n != numLenBytes {
		return Decbuf{E: errors.Wrapf(ErrInvalidSize, "insufficient bytes read for size (got %d, wanted %d)", n, numLenBytes)}
	}

	contentLength := int(binary.BigEndian.Uint32(lengthBytes))
	bufferLength := len(lengthBytes) + contentLength + crc32.Size
	r, err := NewFileReader(f, offset, bufferLength, df.files)
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "create file reader")}
	}

	closeFile = false
	d := Decbuf{r: r}

	if d.ResetAt(numLenBytes); d.Err() != nil {
		return d
	}

	if table != nil {
		if d.CheckCrc32(table); d.Err() != nil {
			return d
		}

		// reset to the beginning of the content after reading it all for the CRC.
		d.ResetAt(numLenBytes)
	}

	return d
}

func (df *FilePoolDecbufFactory) NewDecbufAtUnchecked(offset int) Decbuf {
	return df.NewDecbufAtChecked(offset, nil)
}

// NewDecbufInSection creates a FileReader bounded to the absolute byte range
// [tableOffset+sectionStartOffset, tableOffset+sectionEndOffset). No length
// prefix is read; the caller supplies explicit bounds.
func (df *FilePoolDecbufFactory) NewDecbufInSection(tableOffset, sectionStartOffset, sectionEndOffset int) Decbuf {
	f, err := df.files.Get()
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "open file for decbuf")}
	}

	closeFile := true
	defer func() {
		if closeFile {
			_ = df.files.Put(f)
		}
	}()

	base := tableOffset + sectionStartOffset
	requestedLength := sectionEndOffset - sectionStartOffset

	// Clamp to the actual number of available bytes so that Len() is always accurate
	// and reads never attempt to go past the end of the file.
	stat, err := f.Stat()
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "stat file for section decbuf")}
	}
	available := int(stat.Size()) - base
	if available < 0 {
		available = 0
	}
	length := min(requestedLength, available)

	r, err := NewFileReader(f, base, length, df.files)
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "create file reader")}
	}

	closeFile = false
	return Decbuf{r: r}
}

// NewDecbufUvarintAt reads a uvarint content-length at offset, verifies the CRC32 of the
// content (if table is non-nil), and returns a Decbuf bounded to the content bytes only.
// This mirrors the behaviour of prometheus/tsdb/encoding.NewDecbufUvarintAt.
func (df *FilePoolDecbufFactory) NewDecbufUvarintAt(offset int, table *crc32.Table) Decbuf {
	f, err := df.files.Get()
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "open file for decbuf")}
	}

	closeFile := true
	defer func() {
		if closeFile {
			_ = df.files.Put(f)
		}
	}()

	// Read enough bytes to decode the uvarint length prefix (up to 10 bytes for uint64).
	var lenBuf [binary.MaxVarintLen64]byte
	if _, err := f.ReadAt(lenBuf[:], int64(offset)); err != nil {
		return Decbuf{E: errors.Wrap(err, "read uvarint length prefix")}
	}

	l, n := binary.Uvarint(lenBuf[:])
	if n <= 0 || n > binary.MaxVarintLen64 {
		return Decbuf{E: fmt.Errorf("invalid uvarint length prefix at offset %d: n=%d", offset, n)}
	}

	if table != nil {
		// Read content + 4-byte CRC into a temporary buffer for inline verification.
		// This mirrors prometheus/tsdb/encoding.NewDecbufUvarintAt, which checks the CRC
		// before returning the Decbuf.
		contentAndCRC := make([]byte, int(l)+crc32.Size)
		if _, err := f.ReadAt(contentAndCRC, int64(offset+n)); err != nil {
			return Decbuf{E: errors.Wrap(err, "read content for CRC verification")}
		}
		actual := crc32.Checksum(contentAndCRC[:l], table)
		expected := binary.BigEndian.Uint32(contentAndCRC[l:])
		if actual != expected {
			return Decbuf{E: ErrInvalidChecksum}
		}
	}

	// Create a FileReader bounded to content only (length l, no CRC bytes).
	r, err := NewFileReader(f, offset+n, int(l), df.files)
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "create file reader")}
	}

	closeFile = false
	return Decbuf{r: r}
}

func (df *FilePoolDecbufFactory) NewRawDecbuf() Decbuf {
	f, err := df.files.Get()
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "open file for decbuf")}
	}

	// If we return early and don't include a BufReader for our Decbuf, we are responsible
	// for putting the file handle back in the pool.
	closeFile := true
	defer func() {
		if closeFile {
			_ = df.files.Put(f)
		}
	}()

	stat, err := f.Stat()
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "stat file for decbuf")}
	}

	fileSize := stat.Size()
	reader, err := NewFileReader(f, 0, int(fileSize), df.files)
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "file reader for decbuf")}
	}

	closeFile = false
	return Decbuf{r: reader}
}

// Close cleans up resources associated with this DecbufFactory
func (df *FilePoolDecbufFactory) Close() error {
	df.files.Stop()
	return nil
}
