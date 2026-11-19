package postings_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestReader_ReadBeforeOpen(t *testing.T) {
	r := postings.NewReader(postings.ReaderOptions{
		Allocator: memory.DefaultAllocator,
	})
	rec, err := r.Read(context.Background(), 128)
	require.Nil(t, rec)
	require.ErrorContains(t, err, "reader not opened")
}

func TestReader_RoundTrip(t *testing.T) {
	// Build a tiny postings section with one label entry, read it back.
	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()
	b.ObserveLabelPosting(postings.LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Time: postings.At(ts), UncompressedSize: 100})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err = postings.Open(context.Background(), s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(context.Background()))
	t.Cleanup(func() { _ = r.Close() })

	tbl, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, tbl)
	require.NoError(t, err)

	require.Len(t, actual, 1)
	row := actual[0]
	require.Equal(t, int64(postings.KindLabel), row["kind.int64"])
	require.Equal(t, "/obj", row["object_path.utf8"])
	require.Equal(t, int64(0), row["section_index.int64"])
	require.Equal(t, "env", row["column_name.utf8"])
	require.Equal(t, "prod", row["label_value.utf8"])
	require.Equal(t, int64(100), row["uncompressed_size.int64"])
	require.Equal(t, ts.UTC(), row["min_timestamp.timestamp"])
	require.Equal(t, ts.UTC(), row["max_timestamp.timestamp"])
	require.Equal(t, int64(0), row["min_shard_bucket.int64"])
	require.Equal(t, int64(0), row["max_shard_bucket.int64"])
}

// readTable drains a Reader into an arrow.Table, mirroring the helper in
// streams/reader_test.go.
func readTable(ctx context.Context, r *postings.Reader) (arrow.Table, error) {
	var recs []arrow.RecordBatch
	for {
		rec, err := r.Read(ctx, 128)
		if rec != nil && rec.NumRows() > 0 {
			recs = append(recs, rec)
		}
		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	if len(recs) == 0 {
		return nil, io.EOF
	}
	return array.NewTableFromRecords(recs[0].Schema(), recs), nil
}

// countingBucket counts the bytes that range reads return.
type countingBucket struct {
	*objstore.InMemBucket
	bytesRead atomic.Int64
}

func (b *countingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	rc, err := b.InMemBucket.GetRange(ctx, name, off, length)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(rc)
	if closeErr := rc.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	b.bytesRead.Add(int64(len(data)))
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestReader_StreamPages(t *testing.T) {
	const (
		blooms    = 4096
		bloomSize = 16 << 10
	)
	ctx := context.Background()

	// The bloom filter column is not compressed, so the section holds about
	// blooms*bloomSize bytes. A row reader reads 1024 rows at a time, so the
	// first batch needs a quarter of the section.
	builder := postings.NewMergeBuilder(nil, 128<<10, 0, 1<<30)
	builder.SetTenant("acme")
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range blooms {
		bloom := make([]byte, bloomSize)
		for j := range bloom {
			bloom[j] = byte(rng.Uint32())
		}
		require.NoError(t, builder.AppendBloomEntry(postings.BloomEntry{
			ObjectPath: "logs/a", SectionIndex: int64(i), ColumnName: "trace_id",
			BloomFilter: bloom, StreamIDBitmap: []byte{1},
		}))
	}
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(builder))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer func() { require.NoError(t, closer.Close()) }()
	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, reader.Close())
	require.NoError(t, err)

	// readFirstRow opens the postings section of the object through a counting
	// bucket and reads its first row. It returns the bytes that the bucket
	// returned for the row, and the number of rows of the section.
	readFirstRow := func(t *testing.T, streamPages bool) (bytesForFirstRow int64, rows int) {
		t.Helper()
		bucket := &countingBucket{InMemBucket: objstore.NewInMemBucket()}
		require.NoError(t, bucket.Upload(ctx, "index", bytes.NewReader(data)))
		opened, err := dataobj.FromBucket(ctx, bucket, "index", 0)
		require.NoError(t, err)
		var section *postings.Section
		for _, sec := range opened.Sections() {
			if postings.CheckSection(sec) {
				section, err = postings.Open(ctx, sec)
				require.NoError(t, err)
			}
		}
		require.NotNil(t, section)

		before := bucket.bytesRead.Load()
		r := postings.NewReader(postings.ReaderOptions{Columns: section.Columns(), StreamPages: streamPages})
		require.NoError(t, r.Open(ctx))
		rowReader := postings.NewRowReader(ctx, r)
		defer func() { require.NoError(t, rowReader.Close()) }()
		require.True(t, rowReader.Next())
		bytesForFirstRow = bucket.bytesRead.Load() - before
		for rows = 1; rowReader.Next(); rows++ {
		}
		require.NoError(t, rowReader.Err())
		return bytesForFirstRow, rows
	}

	t.Run("downloads the whole section before the first row without streaming pages", func(t *testing.T) {
		downloaded, rows := readFirstRow(t, false)
		require.Equal(t, blooms, rows)
		require.Greater(t, downloaded, int64(blooms*bloomSize*9/10))
	})

	t.Run("downloads a bounded part of the section before the first row and reads every row when it streams pages", func(t *testing.T) {
		downloaded, rows := readFirstRow(t, true)
		require.Equal(t, blooms, rows)
		require.Less(t, downloaded, int64(blooms*bloomSize/2))
	})
}
