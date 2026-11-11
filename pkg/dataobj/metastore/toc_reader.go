package metastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
)

// ReadWatermarks returns the watermarks of the tenant's ToC for the metastore
// window that contains the given time. It rounds an unaligned time down to the
// start of its window.
//
// ReadWatermarks reads the ToC from the bucket on every call, with no cache.
// It does not use [TOCResolver], because the resolver serves ToCs through
// caches, so its data can be stale. A stale watermark can be lower than the
// real one, and the next conditional write then fails with
// [ErrWatermarkMismatch].
//
// ReadWatermarks reads only the footer and the watermarks section. Its cost
// does not grow with the number of index pointers.
//
// It returns a nil slice and a nil error if the ToC does not exist or holds no
// watermarks section. It returns an error if the ToC does not hold exactly the
// given tenant. That error wraps an unrecoverable ToC error, the same one
// [TableOfContentsWriter] returns.
//
// The order of the rows is unspecified. The caller owns the returned slice.
func ReadWatermarks(ctx context.Context, bucket objstore.BucketReader, tenant string, window time.Time) ([]watermarks.Watermark, error) {
	window = window.Truncate(MetastoreWindowSize).UTC()
	tocPath := TableOfContentsPath(tenant, window)

	tocObject, err := dataobj.FromBucket(ctx, bucket, tocPath, 0)
	if err != nil {
		if isNotFound(bucket, err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening ToC %s: %w", tocPath, err)
	}

	if err := checkTenant(tocObject, tenant); err != nil {
		return nil, fmt.Errorf("checking tenant of ToC %s: %w", tocPath, err)
	}

	var rows []watermarks.Watermark
	for res := range watermarks.Iter(ctx, tocObject) {
		wm, err := res.Value()
		if err != nil {
			return nil, fmt.Errorf("reading watermarks of ToC %s: %w", tocPath, err)
		}
		rows = append(rows, wm)
	}
	return rows, nil
}

// ReadWatermark returns the watermark of one topic and partition in the
// tenant's ToC for the metastore window that contains the given time. It
// returns nil if the ToC holds no row for the topic and partition.
//
// A nil result matches a nil [WatermarkUpdate.Expected], so the caller can
// pass the result as Expected. The caller owns the returned pointer.
//
// ReadWatermark reads the bucket and returns errors like [ReadWatermarks].
func ReadWatermark(ctx context.Context, bucket objstore.BucketReader, tenant string, window time.Time, topic string, partition int32) (*int64, error) {
	rows, err := ReadWatermarks(ctx, bucket, tenant, window)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Topic == topic && row.Partition == partition {
			value := row.Watermark
			return &value, nil
		}
	}
	return nil, nil
}

// isNotFound reports whether err, or an error it wraps, is a not-found error
// of bucket. It checks each error of the chain, because some bucket
// implementations match only the error their own client returns, not one that
// a caller wrapped with %w.
func isNotFound(bucket objstore.BucketReader, err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if bucket.IsObjNotFoundErr(err) {
			return true
		}
	}
	return false
}
