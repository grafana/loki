package store

import (
	"context"
	"fmt"
	"io"

	"github.com/thanos-io/objstore"
)

// bucketReaderAt implements io.ReaderAt by issuing range reads against an
// object storage bucket. This allows IndexReader to query indexes without
// downloading the full file — each ReadAt call maps to a single
// Bucket.GetRange request.
type bucketReaderAt struct {
	bucket objstore.Bucket
	path   string
	ctx    context.Context
}

// NewBucketReaderAt returns an io.ReaderAt backed by range reads against
// the given bucket path.
func NewBucketReaderAt(ctx context.Context, bucket objstore.Bucket, path string) io.ReaderAt {
	return &bucketReaderAt{
		bucket: bucket,
		path:   path,
		ctx:    ctx,
	}
}

func (r *bucketReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	rc, err := r.bucket.GetRange(r.ctx, r.path, off, int64(len(p)))
	if err != nil {
		return 0, fmt.Errorf("bucket range read %s offset=%d len=%d: %w", r.path, off, len(p), err)
	}
	defer rc.Close()

	return io.ReadFull(rc, p)
}
