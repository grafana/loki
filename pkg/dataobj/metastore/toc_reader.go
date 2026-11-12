package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
	"github.com/grafana/loki/v3/pkg/util"
)

// maxTableOfContentsSize is the largest ToC, in bytes, that readTableOfContents reads. It bounds the memory of one read,
// because the read holds the whole ToC in memory.
const maxTableOfContentsSize = 256 << 20

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
// ReadWatermarks reads the whole ToC, so its cost grows with the number of
// index pointers.
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

	tocObject, err := OpenTableOfContents(ctx, bucket, tocPath)
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

// OpenTableOfContents reads the ToC at path from bucket and returns it as a data object.
//
// OpenTableOfContents reads the ToC with one Get, and the returned object reads only those bytes. So every read of the
// object sees one version of the ToC. The writer replaces a ToC in place, so the range reads of [dataobj.FromBucket]
// can each see a different version.
//
// When the ToC does not exist, bucket.IsObjNotFoundErr reports the error. An existing ToC with no bytes, or with more
// than 256 MiB, is an error.
func OpenTableOfContents(ctx context.Context, bucket objstore.BucketReader, path string) (*dataobj.Object, error) {
	data, err := readTableOfContents(ctx, bucket, path)
	if err != nil {
		return nil, err
	}
	object, err := decodeTableOfContents(data)
	if err != nil {
		return nil, fmt.Errorf("decoding ToC %s: %w", path, err)
	}
	return object, nil
}

// IsTableOfContentsPath reports whether path names a ToC. path is relative to the index bucket or to its parent, so it
// starts with [TocPrefix] or with [IndexStoragePrefix] and then [TocPrefix].
func IsTableOfContentsPath(path string) bool {
	path = strings.TrimPrefix(path, IndexStoragePrefix+"/")
	return strings.HasPrefix(path, TocPrefix) && strings.HasSuffix(path, "/"+tocFileName)
}

// readTableOfContents returns the bytes of the ToC at path, read with one Get. When the ToC does not exist,
// bucket.IsObjNotFoundErr reports the error. A ToC larger than maxTableOfContentsSize is an error.
func readTableOfContents(ctx context.Context, bucket objstore.BucketReader, path string) (data []byte, err error) {
	rc, err := bucket.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer util.CloseAndHandleError(rc, &err)

	// The object storage providers take the size from the Get response, so TryToGetSize sends no request.
	size, sizeErr := objstore.TryToGetSize(rc)
	if sizeErr != nil {
		size = -1
	}

	data, err = readAtMost(rc, size, maxTableOfContentsSize)
	if err != nil {
		return nil, fmt.Errorf("reading metastore object %s: %w", path, err)
	}

	// A ToC that exists is never empty. A store that truncates an object before it writes the new one can serve an
	// empty read. That read must not count as a missing ToC.
	if len(data) == 0 {
		return nil, fmt.Errorf("metastore object %s is empty", path)
	}
	return data, nil
}

// readAtMost returns all bytes of r. It returns an error when r holds more than limit bytes.
//
// size is the number of bytes in r, or a negative value when it is unknown. A known size makes the read allocate
// exactly size bytes. Then r must hold exactly size bytes. A size above limit is an error before the read.
func readAtMost(r io.Reader, size, limit int64) ([]byte, error) {
	if size > limit {
		return nil, fmt.Errorf("the reader has %d bytes, more than the limit of %d bytes", size, limit)
	}

	if size < 0 {
		// The extra byte tells a reader with limit bytes apart from a reader with more.
		data, err := io.ReadAll(io.LimitReader(r, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("the reader has more than the limit of %d bytes", limit)
		}
		return data, nil
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("reading %d bytes: %w", size, err)
	}
	var extra [1]byte
	switch _, err := io.ReadFull(r, extra[:]); {
	case err == io.EOF:
		return data, nil
	case err == nil:
		return nil, fmt.Errorf("the reader has more than its size of %d bytes", size)
	default:
		return nil, fmt.Errorf("reading past %d bytes: %w", size, err)
	}
}

// decodeTableOfContents returns the ToC in data as a data object. The object reads only data and keeps a reference to
// it, so the caller must not modify data while it uses the object.
func decodeTableOfContents(data []byte) (*dataobj.Object, error) {
	return dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
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
