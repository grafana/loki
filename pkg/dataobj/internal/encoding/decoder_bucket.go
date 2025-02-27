package encoding

import (
	"context"
	"fmt"
	"io"

	"github.com/thanos-io/objstore"
)

// BucketDecoder decodes a data object from the provided path within the
// specified [objstore.BucketReader].
func BucketDecoder(bucket objstore.BucketReader, path string) Decoder {
	return &rangeDecoder{
		r: &bucketRangeReader{bucket: bucket, path: path},
	}
}

type bucketRangeReader struct {
	bucket objstore.BucketReader
	path   string
}

func (rr *bucketRangeReader) Size(ctx context.Context) (int64, error) {
	attrs, err := rr.bucket.Attributes(ctx, rr.path)
	if err != nil {
		return 0, fmt.Errorf("reading attributes: %w", err)
	}
	return attrs.Size, nil
}

func (rr *bucketRangeReader) ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error) {
	return rr.bucket.GetRange(ctx, rr.path, offset, length)
}
