package compactor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

type multiOutputIndex struct {
	CompactedIndex
	files []index.Index
}

func (m multiOutputIndex) ToIndexFiles() ([]index.Index, error) { return m.files, nil }

type outputFile struct {
	path   string
	closed bool
}

func (f *outputFile) Name() string                       { return filepath.Base(f.path) }
func (f *outputFile) Path() string                       { return f.path }
func (f *outputFile) Close() error                       { f.closed = true; return nil }
func (f *outputFile) Reader() (io.ReadSeekCloser, error) { return os.Open(f.path) }

type uploadRecorder struct {
	storage.IndexSet
	names  []string
	failAt int
}

func (r *uploadRecorder) PutFile(_ context.Context, _, _, name string, _ io.ReadSeeker) error {
	r.names = append(r.names, name)
	if len(r.names) == r.failAt {
		return errors.New("upload failed")
	}
	return nil
}
func TestIndexSetMultiOutputCleanup(t *testing.T) {
	for _, failAt := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("failAt=%d", failAt), func(t *testing.T) {
			var files []index.Index
			for i := 0; i < 3; i++ {
				path := filepath.Join(t.TempDir(), fmt.Sprintf("shard-%d", i))
				require.NoError(t, os.WriteFile(path, []byte("index"), 0600))
				files = append(files, &outputFile{path: path})
			}
			storage := &uploadRecorder{failAt: failAt}
			set := &indexSet{ctx: context.Background(), baseIndexSet: storage, compactedIndex: multiOutputIndex{files: files}, logger: log.NewNopLogger()}
			err := set.upload()
			if failAt == 0 {
				require.NoError(t, err)
				require.Len(t, storage.names, 3)
			} else {
				require.ErrorContains(t, err, "upload failed")
				require.Len(t, storage.names, failAt)
			}
			for _, f := range files {
				require.True(t, f.(*outputFile).closed)
				_, err := os.Stat(f.Path())
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}
