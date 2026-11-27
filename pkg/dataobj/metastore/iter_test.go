package metastore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/memory"
)

const (
	iterTenant = "tenant-1"

	// iterBatchSize is the number of pointers that forEachIndexPointer reads at a
	// time. numPointers spans three batches.
	iterBatchSize = 1024
	numPointers   = 2500
)

func TestForEachIndexPointer(t *testing.T) {
	t.Run("returns every pointer of a section that spans several batches", func(t *testing.T) {
		want := newPointers(t, numPointers, func(int) bool { return false })
		obj := buildPointersObject(t, pointersSection{tenant: iterTenant, pointers: want})

		require.Equal(t, want, readAllPointers(t, obj))
	})

	t.Run("does not carry optional fields over from the previous batch", func(t *testing.T) {
		want := newPointers(t, numPointers, func(i int) bool { return i < iterBatchSize })
		obj := buildPointersObject(t, pointersSection{tenant: iterTenant, pointers: want})

		require.Equal(t, want, readAllPointers(t, obj))
	})

	t.Run("returns pointers that stay valid after the allocator reuses its memory", func(t *testing.T) {
		want := newPointers(t, numPointers, func(int) bool { return true })
		obj := buildPointersObject(t, pointersSection{tenant: iterTenant, pointers: want})

		require.Equal(t, want, readAllPointers(t, obj))
	})

	t.Run("returns only the pointers that overlap the time range, bounds included", func(t *testing.T) {
		all := newPointers(t, numPointers, func(int) bool { return false })
		obj := buildPointersObject(t, pointersSection{tenant: iterTenant, pointers: all})

		first, last := 49, 100
		got := readPointers(t, obj, iterTenant, all[first].EndTs.Unix(), all[last].StartTs.Unix())

		require.Equal(t, all[first:last+1], got)
	})

	t.Run("returns the pointers of every section of the tenant", func(t *testing.T) {
		all := newPointers(t, numPointers, func(i int) bool { return i%2 == 0 })
		obj := buildPointersObject(t,
			pointersSection{tenant: iterTenant, pointers: all[:1200]},
			pointersSection{tenant: iterTenant, pointers: all[1200:]},
		)

		require.Equal(t, all, readAllPointers(t, obj))
	})

	t.Run("skips the sections of other tenants", func(t *testing.T) {
		all := newPointers(t, numPointers, func(int) bool { return false })
		other := newPointers(t, 10, func(int) bool { return true })
		obj := buildPointersObject(t,
			pointersSection{tenant: "tenant-2", pointers: other},
			pointersSection{tenant: iterTenant, pointers: all},
		)

		require.Equal(t, all, readAllPointers(t, obj))
		require.Equal(t, other, readPointers(t, obj, "tenant-2", 0, 2*numPointers))
	})

	t.Run("returns an error and no pointer when the context has no tenant", func(t *testing.T) {
		obj := buildPointersObject(t, pointersSection{
			tenant:   iterTenant,
			pointers: newPointers(t, 10, func(int) bool { return false }),
		})

		var calls int
		err := forEachIndexPointer(t.Context(), obj, timestamp(0), timestamp(2*numPointers), func(indexpointers.IndexPointer) {
			calls++
		})
		require.EqualError(t, err, "extracting org ID: no org id")
		require.Zero(t, calls)
	})

	t.Run("returns the error of a read that fails", func(t *testing.T) {
		obj := buildPointersObject(t, pointersSection{
			tenant:   iterTenant,
			pointers: newPointers(t, numPointers, func(int) bool { return false }),
		})

		ctx, cancel := context.WithCancel(user.InjectOrgID(t.Context(), iterTenant))
		t.Cleanup(cancel)

		var calls int
		err := forEachIndexPointer(ctx, obj, timestamp(0), timestamp(2*numPointers), func(indexpointers.IndexPointer) {
			calls++
			cancel()
		})
		require.EqualError(t, err, "reading recordBatch: reading record batch: context canceled")
		require.GreaterOrEqual(t, calls, 1)
		require.LessOrEqual(t, calls, iterBatchSize)
	})
}

func TestDecodePointers(t *testing.T) {
	buckets := indexpointers.NewShardBuckets(streams.MustShardLayoutFromBuckets(64))
	keys, err := postings.NewKeyRange(
		postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "a"},
		postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "b"},
	)
	require.NoError(t, err)
	minKey, maxKey := keys.Encode()

	// validBatch returns a batch of two rows with every column that decodePointers
	// reads.
	validBatch := func() *indexpointers.RecordBatch {
		return &indexpointers.RecordBatch{
			Rows:          2,
			Path:          pointerStrings(nil, []byte("first"), []byte("second")),
			MinTimestamp:  pointerNumbers(nil, 1, 3),
			MaxTimestamp:  pointerNumbers(nil, 2, 4),
			RunID:         pointerNumbers(nil, 5, 6),
			RunLength:     pointerNumbers(nil, 2, 3),
			ShardBuckets:  pointerNumbers(nil, 64, 64),
			MinPostingKey: pointerStrings(nil, minKey, minKey),
			MaxPostingKey: pointerStrings(nil, maxKey, maxKey),
		}
	}
	wantValid := []indexpointers.IndexPointer{
		{Path: "first", StartTs: time.Unix(0, 1), EndTs: time.Unix(0, 2), RunID: 5, RunLength: 2, ShardBuckets: buckets, PostingKeys: keys},
		{Path: "second", StartTs: time.Unix(0, 3), EndTs: time.Unix(0, 4), RunID: 6, RunLength: 3, ShardBuckets: buckets, PostingKeys: keys},
	}

	t.Run("decodes every field of every row", func(t *testing.T) {
		buf := make([]indexpointers.IndexPointer, 2)

		require.NoError(t, decodePointers(validBatch(), buf))
		require.Equal(t, wantValid, buf)
	})

	t.Run("leaves the fields of a null element at their zero value when its slot holds other data", func(t *testing.T) {
		batch := &indexpointers.RecordBatch{
			Rows:          2,
			Path:          pointerStrings([]bool{false, true}, []byte("junk"), []byte("ok")),
			MinTimestamp:  pointerNumbers([]bool{false, true}, 111, 5),
			MaxTimestamp:  pointerNumbers([]bool{false, true}, 222, 6),
			RunID:         pointerNumbers([]bool{false, true}, 9, 3),
			RunLength:     pointerNumbers([]bool{false, true}, 8, 4),
			ShardBuckets:  pointerNumbers([]bool{false, true}, 7, 64),
			MinPostingKey: pointerStrings([]bool{false, true}, []byte("junk"), minKey),
			MaxPostingKey: pointerStrings([]bool{false, true}, []byte("junk"), maxKey),
		}
		buf := []indexpointers.IndexPointer{{Path: "stale", RunID: 99}, {Path: "stale", RunID: 99}}

		require.NoError(t, decodePointers(batch, buf))
		require.Equal(t, indexpointers.IndexPointer{}, buf[0])
		require.Equal(t, indexpointers.IndexPointer{
			Path:         "ok",
			StartTs:      time.Unix(0, 5),
			EndTs:        time.Unix(0, 6),
			RunID:        3,
			RunLength:    4,
			ShardBuckets: buckets,
			PostingKeys:  keys,
		}, buf[1])
	})

	t.Run("leaves the fields of a column that the batch does not hold at their zero value", func(t *testing.T) {
		batch := &indexpointers.RecordBatch{Rows: 1, Path: pointerStrings(nil, []byte("only"))}
		buf := []indexpointers.IndexPointer{{Path: "stale", RunID: 99}}

		require.NoError(t, decodePointers(batch, buf))
		require.Equal(t, []indexpointers.IndexPointer{{Path: "only"}}, buf)
	})

	t.Run("returns pointers that stay valid after the memory of the batch changes", func(t *testing.T) {
		batch := validBatch()
		buf := make([]indexpointers.IndexPointer, 2)
		require.NoError(t, decodePointers(batch, buf))

		zeroPointerArrays(batch)

		require.Equal(t, wantValid, buf)
	})

	for _, tc := range []struct {
		name  string
		batch *indexpointers.RecordBatch
		want  string
	}{
		{
			name:  "returns an error for a run ID that is not positive",
			batch: &indexpointers.RecordBatch{Rows: 1, RunID: pointerNumbers(nil, 0)},
			want:  "decoding index pointer 0: invalid run_id value 0",
		},
		{
			name:  "returns an error for a run length that is not positive",
			batch: &indexpointers.RecordBatch{Rows: 1, RunLength: pointerNumbers(nil, 0)},
			want:  "decoding index pointer 0: invalid run_length value 0",
		},
		{
			name:  "returns an error for a shard bucket count that is not valid",
			batch: &indexpointers.RecordBatch{Rows: 1, ShardBuckets: pointerNumbers(nil, 0)},
			want:  "decoding index pointer 0: invalid shard_buckets value 0",
		},
		{
			name: "returns an error for a posting key range with only one bound",
			batch: &indexpointers.RecordBatch{
				Rows:          1,
				MinPostingKey: pointerStrings(nil, minKey),
				MaxPostingKey: pointerStrings([]bool{false}, []byte("junk")),
			},
			want: "decoding index pointer 0: posting key range has only one bound: min=true max=false",
		},
		{
			name: "returns an error for a posting key range with empty bounds",
			batch: &indexpointers.RecordBatch{
				Rows:          1,
				MinPostingKey: pointerStrings(nil, []byte{}),
				MaxPostingKey: pointerStrings(nil, []byte{}),
			},
			want: "decoding index pointer 0: decoding min_posting_key and max_posting_key: decoding min bound: invalid posting key encoding: no kind",
		},
		{
			name:  "returns the position of the row that does not decode",
			batch: &indexpointers.RecordBatch{Rows: 2, RunID: pointerNumbers(nil, 1, -1)},
			want:  "decoding index pointer 1: invalid run_id value -1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.EqualError(t, decodePointers(tc.batch, make([]indexpointers.IndexPointer, tc.batch.Rows)), tc.want)
		})
	}
}

func TestBinaryValue(t *testing.T) {
	var (
		values = columnartest.Array(t, types.KindUTF8, nil, "bound", nil).(*columnar.UTF8)

		// The only value is empty and data is nil, so Get returns a nil slice.
		empty = columnar.NewUTF8(nil, []int32{0, 0}, memory.Bitmap{})
	)

	t.Run("returns nil when the array is nil", func(t *testing.T) {
		require.Nil(t, binaryValue(nil, 0))
	})

	t.Run("returns nil when the value is null", func(t *testing.T) {
		require.Nil(t, binaryValue(values, 1))
	})

	t.Run("returns the bytes of the value", func(t *testing.T) {
		require.Equal(t, []byte("bound"), binaryValue(values, 0))
	})

	t.Run("returns an empty slice that is not nil when the value is empty", func(t *testing.T) {
		got := binaryValue(empty, 0)
		require.NotNil(t, got)
		require.Empty(t, got)
	})
}

// newPointers returns count pointers with growing timestamps. A pointer has the
// optional fields when optional returns true for its index. The run ID and the
// posting keys depend on the index, so no two pointers share them.
func newPointers(t *testing.T, count int, optional func(i int) bool) []indexpointers.IndexPointer {
	t.Helper()

	buckets := indexpointers.NewShardBuckets(streams.MustShardLayoutFromBuckets(64))
	pointers := make([]indexpointers.IndexPointer, count)
	for i := range pointers {
		pointers[i] = indexpointers.IndexPointer{
			Path:    fmt.Sprintf("indexes/%05d", i),
			StartTs: time.Unix(int64(2*i), 0).UTC(),
			EndTs:   time.Unix(int64(2*i+1), 0).UTC(),
		}
		if !optional(i) {
			continue
		}

		keys, err := postings.NewKeyRange(
			postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: fmt.Sprintf("a-%05d", i)},
			postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: fmt.Sprintf("b-%05d", i)},
		)
		require.NoError(t, err)
		pointers[i].RunID = indexpointers.RunID(i + 1)
		pointers[i].RunLength = int32(i%3 + 1)
		pointers[i].ShardBuckets = buckets
		pointers[i].PostingKeys = keys
	}
	return pointers
}

// pointersSection is an index pointers section of a tenant.
type pointersSection struct {
	tenant   string
	pointers []indexpointers.IndexPointer
}

// buildPointersObject returns an object with one index pointers section for each
// of the sections, in order.
func buildPointersObject(t *testing.T, sections ...pointersSection) *dataobj.Object {
	t.Helper()

	objBuilder := dataobj.NewBuilder(nil)
	for _, section := range sections {
		builder := indexpointers.NewBuilder(nil, 1<<20, 0)
		builder.SetTenant(section.tenant)
		for _, pointer := range section.pointers {
			builder.Append(pointer)
		}
		require.NoError(t, objBuilder.Append(builder))
	}

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	return obj
}

// readAllPointers returns the pointers of iterTenant that overlap the whole
// time span of newPointers.
func readAllPointers(t *testing.T, obj *dataobj.Object) []indexpointers.IndexPointer {
	t.Helper()

	return readPointers(t, obj, iterTenant, 0, 2*numPointers)
}

// readPointers returns the pointers that forEachIndexPointer reports for obj and
// tenant, between startSec and endSec. It sets their timestamps to UTC and keeps
// every pointer until the iteration ends.
func readPointers(t *testing.T, obj *dataobj.Object, tenant string, startSec, endSec int64) []indexpointers.IndexPointer {
	t.Helper()

	var got []indexpointers.IndexPointer
	ctx := user.InjectOrgID(t.Context(), tenant)
	err := forEachIndexPointer(ctx, obj, timestamp(startSec), timestamp(endSec), func(pointer indexpointers.IndexPointer) {
		pointer.StartTs = pointer.StartTs.UTC()
		pointer.EndTs = pointer.EndTs.UTC()
		got = append(got, pointer)
	})
	require.NoError(t, err)
	return got
}

func timestamp(sec int64) *scalar.Timestamp {
	return scalar.NewTimestampScalar(arrow.Timestamp(time.Unix(sec, 0).UnixNano()), &arrow.TimestampType{Unit: arrow.Nanosecond})
}

// pointerNumbers returns an array of the values. The elements for which valid is
// false are null, but they keep their values in the array. A nil valid means
// that no element is null.
func pointerNumbers(valid []bool, values ...int64) *columnar.Number[int64] {
	return columnar.NewNumber(values, pointerValidity(valid))
}

// pointerStrings returns an array of the values. The elements for which valid is
// false are null, but they keep their bytes in the array. A nil valid means
// that no element is null.
func pointerStrings(valid []bool, values ...[]byte) *columnar.UTF8 {
	var data []byte
	offsets := []int32{0}
	for _, value := range values {
		data = append(data, value...)
		offsets = append(offsets, int32(len(data)))
	}
	return columnar.NewUTF8(data, offsets, pointerValidity(valid))
}

func pointerValidity(valid []bool) memory.Bitmap {
	validity := memory.NewBitmap(nil, len(valid))
	validity.AppendValues(valid...)
	return validity
}

// zeroPointerArrays zeroes the data, the offsets and the values of every array
// of batch. It models the reuse of that memory by the allocator after Reclaim.
func zeroPointerArrays(batch *indexpointers.RecordBatch) {
	for _, values := range []*columnar.UTF8{batch.Path, batch.MinPostingKey, batch.MaxPostingKey} {
		if values != nil {
			clear(values.Data())
			clear(values.Offsets())
		}
	}
	for _, numbers := range []*columnar.Number[int64]{batch.MinTimestamp, batch.MaxTimestamp, batch.RunID, batch.ShardBuckets, batch.RunLength} {
		if numbers != nil {
			clear(numbers.Values())
		}
	}
}
