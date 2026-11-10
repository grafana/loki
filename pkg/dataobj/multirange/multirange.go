// Package multirange reads many ranges of one object in one call.
package multirange

import (
	"context"
	"io"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

// Bucket is an [objstore.Bucket] that also reads many ranges of one object in one call.
type Bucket interface {
	objstore.Bucket

	// MultiGetRange fills ranges[i].Data from the object, starting at ranges[i].Offset. It reads exactly
	// len(Data) bytes of each range, or returns an error. It may read the ranges in parallel and in any
	// order, and it ignores empty ranges.
	//
	// MultiGetRange can return an error that wraps [io.EOF], for example when a range lies beyond the end
	// of the object. A caller that reads data of a known size must treat that error as a failure, not as
	// the end of the data. When MultiGetRange returns an error, the buffers can be half filled.
	MultiGetRange(ctx context.Context, name string, ranges []rangeio.Range) error
}

type multiGetter interface {
	MultiGetRange(ctx context.Context, name string, ranges []rangeio.Range) error
}

// Read fills the ranges from the object. It calls MultiGetRange when the bucket has it. Otherwise it
// reads the ranges with GetRange, and coalesces them as [rangeio.ReadRanges] does.
func Read(ctx context.Context, bucket objstore.BucketReader, name string, ranges []rangeio.Range) error {
	if b, ok := bucket.(multiGetter); ok {
		return b.MultiGetRange(ctx, name, ranges)
	}
	return rangeio.ReadRanges(ctx, &getRangeReader{bucket: bucket, name: name}, ranges)
}

// NewCoalescingBucket returns a bucket with MultiGetRange. It merges ranges that are close, splits ranges
// that are big, and reads the result in parallel with GetRange. The config in the context sets the sizes,
// as for [rangeio.ReadRanges]. Every other call goes to bucket unchanged.
func NewCoalescingBucket(bucket objstore.Bucket, reg prometheus.Registerer) Bucket {
	return &coalescingBucket{
		Bucket: bucket,
		neededBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_coalescing_bucket_needed_bytes_total",
			Help: "Bytes in the ranges that callers asked the bucket to read. The bucket reads these bytes and the overread bytes.",
		}),
		overreadBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_coalescing_bucket_overread_bytes_total",
			Help: "Bytes that the bucket read from object storage and no range needed: the gaps between the ranges that it merged.",
		}),
	}
}

type coalescingBucket struct {
	objstore.Bucket

	neededBytes   prometheus.Counter
	overreadBytes prometheus.Counter
}

func (b *coalescingBucket) MultiGetRange(ctx context.Context, name string, ranges []rangeio.Range) error {
	reader := &getRangeReader{bucket: b.Bucket, name: name}
	err := rangeio.ReadRanges(ctx, reader, ranges)

	var needed int64
	for _, r := range ranges {
		needed += r.Len()
	}
	b.neededBytes.Add(float64(needed))
	// Overlapping ranges are read once, so the bytes read can be below the bytes needed.
	b.overreadBytes.Add(float64(max(0, reader.read.Load()-needed)))
	return err
}

// getRangeReader reads the ranges of one object with GetRange.
type getRangeReader struct {
	bucket objstore.BucketReader
	name   string

	read atomic.Int64 // The bytes that the reads returned.
}

func (r *getRangeReader) ReadRange(ctx context.Context, rg rangeio.Range) (int, error) {
	rc, err := r.bucket.GetRange(ctx, r.name, rg.Offset, rg.Len())
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	n, err := io.ReadFull(rc, rg.Data)
	r.read.Add(int64(n))
	return n, err
}

// NewPrefixedBucket returns bucket scoped to prefix, like [objstore.NewPrefixedBucket]. When bucket has
// MultiGetRange, so does the result.
func NewPrefixedBucket(bucket objstore.Bucket, prefix string) objstore.Bucket {
	prefixed := objstore.NewPrefixedBucket(bucket, prefix)
	trimmed := strings.Trim(prefix, objstore.DirDelim)
	inner, ok := bucket.(Bucket)
	if !ok || trimmed == "" {
		return prefixed
	}
	return &prefixedBucket{Bucket: prefixed, inner: inner, prefix: trimmed}
}

type prefixedBucket struct {
	objstore.Bucket

	inner  Bucket
	prefix string
}

func (b *prefixedBucket) MultiGetRange(ctx context.Context, name string, ranges []rangeio.Range) error {
	return b.inner.MultiGetRange(ctx, b.prefix+objstore.DirDelim+name, ranges)
}
