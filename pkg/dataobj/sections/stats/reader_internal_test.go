package stats

import (
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/memory"
)

// BenchmarkStatsReader reads a large stats section with three label columns.
// Many rows share an object path, as the stats rows of an index object do.
func BenchmarkStatsReader(b *testing.B) {
	sec := makeBenchSection(b)

	b.Run("RowReader_full_scan", func(b *testing.B) {
		b.ReportAllocs()

		var rows int
		for b.Loop() {
			rr := NewRowReader(b.Context(), sec, nil)
			for rr.Next() {
				rows++
			}
			require.NoError(b, rr.Err())
			require.NoError(b, rr.Close())
		}
		require.NotZero(b, rows)
		b.ReportMetric(float64(rows)/float64(b.N), "rows/op")
	})

	b.Run("ObjectPaths", func(b *testing.B) {
		column, ok, err := sec.ObjectPathColumn()
		require.NoError(b, err)
		require.True(b, ok)

		alloc := memory.NewAllocator(nil)
		defer alloc.Free()
		b.ReportAllocs()

		for b.Loop() {
			reachable := make(map[string]struct{})
			reader := NewReader(ReaderOptions{Columns: []*Column{column}})
			require.NoError(b, reader.Open(b.Context()))
			for {
				alloc.Reclaim()
				batch, err := reader.ReadRecordBatch(b.Context(), alloc, 1024)
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(b, err)

				for i := range batch.Rows {
					value := batch.ObjectPath.Get(i)
					if _, ok := reachable[string(value)]; ok {
						continue
					}
					require.NoError(b, checkBenchObjectPath(string(value)))
					reachable[string(value)] = struct{}{}
				}
			}
			require.NoError(b, reader.Close())
			require.NotEmpty(b, reachable)
		}
	})
}

// makeBenchSection builds a stats section. Each log object has several sections
// and shard buckets, so many rows share an object path.
func makeBenchSection(b *testing.B) *Section {
	b.Helper()

	const (
		schema   = "label:service_name,label:namespace,label:pod"
		objects  = 2000
		sections = 8
		buckets  = 12
	)
	builder := NewBuilder(nil, ColumnarSectionEncoder(1<<20, 0), testLayout(schema))
	builder.SetTenant("tenant")
	i := 0
	for o := range objects {
		for s := range sections {
			for k := range buckets {
				builder.Append(Stat{
					ObjectPath:   fmt.Sprintf("objects/ab/cdef0123456789%06d", o),
					SectionIndex: int64(s),
					ShardBucket:  uint32(k),
					SortSchema:   schema,
					Labels: map[string]string{
						"service_name": fmt.Sprintf("svc-%d", i%512),
						"namespace":    fmt.Sprintf("ns-%d", i%32),
						"pod":          fmt.Sprintf("pod-%d", i%4096),
					},
					MinTimestamp:     int64(i),
					MaxTimestamp:     int64(i) + 1,
					RowCount:         int64(i),
					UncompressedSize: int64(i) * 10,
				})
				i++
			}
		}
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(b, objBuilder.Append(builder))
	obj, closer, err := objBuilder.Flush()
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, closer.Close()) })

	for _, s := range obj.Sections() {
		if !CheckSection(s) {
			continue
		}
		sec, err := Open(b.Context(), s)
		require.NoError(b, err)
		return sec
	}
	require.FailNow(b, "the object has no stats section")
	return nil
}

// checkBenchObjectPath returns an error unless p is a clean, relative path. It
// repeats the check of the metastore package, which this package cannot import.
func checkBenchObjectPath(p string) error {
	if p != path.Clean(p) || strings.HasPrefix(p, "/") || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("object path %q is not a clean relative path", p)
	}
	return nil
}
