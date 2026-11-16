package tsdb

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

// newTestStore builds a store over a filesystem object store and returns the
// store's base directory, which holds the object store under "objects" and the
// active index directory under "active".
func newTestStore(t *testing.T, mode indexshipper.Mode) (*store, string) {
	t.Helper()
	dir := t.TempDir()

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: filepath.Join(dir, "objects")})
	require.NoError(t, err)

	shipperCfg := indexshipper.Config{}
	flagext.DefaultValues(&shipperCfg)
	shipperCfg.Mode = mode
	shipperCfg.IngesterName = "test"
	shipperCfg.ActiveIndexDirectory = filepath.Join(dir, "active")
	shipperCfg.CacheLocation = filepath.Join(dir, "cache")

	periodCfg := config.PeriodConfig{
		Schema:     "v13",
		IndexType:  types.IndexTypeTSDB,
		ObjectType: types.StorageTypeFileSystem,
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Prefix: "index_", Period: 24 * time.Hour},
		},
	}
	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{periodCfg}}
	tableRange := periodCfg.GetIndexTableNumberRange(config.DayTime{Time: model.Latest})

	rw, stop, err := NewStore("test", "index/", shipperCfg, schemaCfg, objectClient, &zeroValueLimits{}, tableRange, nil, nil, log.NewNopLogger())
	require.NoError(t, err)
	t.Cleanup(stop)
	return rw.(*store), dir
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	}))
	return files
}

func newTestChunk() chunk.Chunk {
	ls := labels.FromStrings("foo", "bar")
	through := model.Now()
	return chunk.Chunk{
		ChunkRef: logproto.ChunkRef{UserID: "user", Fingerprint: labels.StableHash(ls), From: through - 1, Through: through, Checksum: 1},
		Metric:   ls,
		Data:     dummyChunkData{},
	}
}

// Only modes that write index create a WAL; a read-only store refuses to index
// chunks.
func TestStore_HeadOnlyInWritingModes(t *testing.T) {
	for _, tc := range []struct {
		mode      indexshipper.Mode
		wantWrite bool
	}{
		{mode: indexshipper.ModeReadOnly},
		{mode: indexshipper.ModeWriteOnly, wantWrite: true},
		{mode: indexshipper.ModeReadWrite, wantWrite: true},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			s, dir := newTestStore(t, tc.mode)

			walDir := managerWalDir("test", filepath.Join(dir, "active"))
			chk := newTestChunk()
			err := s.IndexChunk(context.Background(), chk.From, chk.Through, chk)
			if tc.wantWrite {
				require.DirExists(t, walDir)
				require.NoError(t, err)
			} else {
				require.NoDirExists(t, walDir)
				require.Error(t, err)
			}
		})
	}
}

// Stop must build the head into TSDB files before stopping the shipper, so
// shutdown uploads whatever was indexed since the last flush.
func TestStore_StopUploadsHead(t *testing.T) {
	for _, mode := range []indexshipper.Mode{indexshipper.ModeWriteOnly, indexshipper.ModeReadWrite} {
		t.Run(string(mode), func(t *testing.T) {
			s, dir := newTestStore(t, mode)
			objectDir := filepath.Join(dir, "objects")
			chk := newTestChunk()
			require.NoError(t, s.IndexChunk(context.Background(), chk.From, chk.Through, chk))
			require.Empty(t, listFiles(t, objectDir))

			s.Stop()

			files := listFiles(t, objectDir)
			require.Len(t, files, 1, "Stop should upload the built TSDB")
			rel, err := filepath.Rel(objectDir, files[0])
			require.NoError(t, err)
			require.Regexp(t, `^index/index_\d+/[^/]+\.tsdb\.gz$`, filepath.ToSlash(rel))
		})
	}
}
