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
	"os"
	"sync"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
)

// FilePoolDecbufFactory creates new file-backed Decbuf instances
// for a specific index-header file on local disk.
type FilePoolDecbufFactory struct {
	files fileHandles
	close func() error
	// fileSize is the size of the file at path in bytes, cached here
	// to avoid repeated stat calls.
	// Index files are immutable once written, so this cannot go stale.
	fileSize int64
}

type fileHandles interface {
	filepool.FilePoolCloser
	Get() (*os.File, error)
}

type pinnedFile struct {
	filepool.SingleFilePoolNoopCloser
	file *os.File
}

func (f pinnedFile) Get() (*os.File, error) { return f.file, nil }

func NewFilePoolDecbufFactory(
	path string,
	maxIdleFileHandles uint,
	metrics *filepool.FilePoolMetrics,
) (*FilePoolDecbufFactory, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, errors.Wrap(err, "stat file for decbuf factory")
	}

	files := filepool.NewFilePool(path, maxIdleFileHandles, metrics)
	return &FilePoolDecbufFactory{
		files: files,
		close: func() error {
			files.Stop()
			return nil
		},
		fileSize: fileInfo.Size(),
	}, nil
}

// Pin borrows a file handle for a scan that must remain readable if the
// file is unlinked. All decbufs from the returned factory share the handle
// via positioned reads and must be closed before closing the factory.
func (df *FilePoolDecbufFactory) Pin() (DecbufFactory, error) {
	file, err := df.files.Get()
	if err != nil {
		return nil, err
	}
	return &FilePoolDecbufFactory{
		files:    pinnedFile{file: file},
		close:    sync.OnceValue(func() error { return df.files.Put(file) }),
		fileSize: df.fileSize,
	}, nil
}

func (df *FilePoolDecbufFactory) FileSize() int64 {
	return df.fileSize
}

func (df *FilePoolDecbufFactory) NewDecbufAtChecked(_ context.Context, offset int, table *crc32.Table) Decbuf {
	f, err := df.files.Get()
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "open file for decbuf")}
	}

	// If we return early without handing the file to a FileReader for our Decbuf, we are
	// responsible for putting the file handle back in the pool.
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

func (df *FilePoolDecbufFactory) NewDecbufAtUnchecked(ctx context.Context, offset int) Decbuf {
	return df.NewDecbufAtChecked(ctx, offset, nil)
}

func (df *FilePoolDecbufFactory) NewDecbufInSection(_ context.Context, _, _, _ int) Decbuf {
	return Decbuf{E: fmt.Errorf("NewDecbufInSection not implemented for FilePoolDecbufFactory")}
}

func (df *FilePoolDecbufFactory) NewRawDecbuf(_ context.Context) Decbuf {
	f, err := df.files.Get()
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "open file for decbuf")}
	}

	// If we return early without handing the file to a FileReader for our Decbuf, we are
	// responsible for putting the file handle back in the pool.
	closeFile := true
	defer func() {
		if closeFile {
			_ = df.files.Put(f)
		}
	}()

	reader, err := NewFileReader(f, 0, int(df.fileSize), df.files)
	if err != nil {
		return Decbuf{E: errors.Wrap(err, "file reader for decbuf")}
	}

	closeFile = false
	return Decbuf{r: reader}
}

// Close cleans up resources associated with this DecbufFactory
func (df *FilePoolDecbufFactory) Close() error {
	return df.close()
}
