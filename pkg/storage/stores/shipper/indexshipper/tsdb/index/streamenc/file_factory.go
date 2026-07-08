// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/file_factory.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Grafana Mimir Authors.

package streamenc

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
)

// FilePoolDecbufFactory creates new file-backed Decbuf instances for a
// specific TSDB index file on local disk.
type FilePoolDecbufFactory struct {
	files *filepool.FilePool
}

func NewFilePoolDecbufFactory(
	path string,
	maxIdleFileHandles uint,
	metrics *filepool.FilePoolMetrics,
) *FilePoolDecbufFactory {
	return &FilePoolDecbufFactory{
		files: filepool.NewFilePool(path, maxIdleFileHandles, metrics),
	}
}

func (df *FilePoolDecbufFactory) NewDecbufAtChecked(_ context.Context, offset int, table *crc32.Table) Decbuf {
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

func (df *FilePoolDecbufFactory) NewDecbufAtUnchecked(ctx context.Context, offset int) Decbuf {
	return df.NewDecbufAtChecked(ctx, offset, nil)
}

// NewDecbufInSection is not implemented for FilePoolDecbufFactory; kept only to satisfy
// the DecbufFactory interface if callers accept it. It will be needed later for the
// posting-offset table streaming optimizations.
func (df *FilePoolDecbufFactory) NewDecbufInSection(_ context.Context, _, _, _ int) Decbuf {
	return Decbuf{E: fmt.Errorf("NewDecbufInSection not implemented for FilePoolDecbufFactory")}
}

func (df *FilePoolDecbufFactory) NewRawDecbuf(_ context.Context) Decbuf {
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

// FileSize returns the current size of the underlying file. It's a
// convenience for callers that need to seek to a tail-relative offset without
// keeping a separate handle open.
func (df *FilePoolDecbufFactory) FileSize() (int64, error) {
	f, err := df.files.Get()
	if err != nil {
		return 0, err
	}
	defer func() { _ = df.files.Put(f) }()
	stat, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return stat.Size(), nil
}

// Close cleans up resources associated with this DecbufFactory.
func (df *FilePoolDecbufFactory) Close() error {
	df.files.Stop()
	return nil
}
