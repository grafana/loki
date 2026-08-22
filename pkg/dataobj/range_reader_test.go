package dataobj

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// erroringBucketReader fails every read. It embeds the interface so the unused methods stay nil.
type erroringBucketReader struct {
	objstore.BucketReader
}

func (erroringBucketReader) Attributes(context.Context, string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, errors.New("boom")
}

func (erroringBucketReader) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("boom")
}

func (erroringBucketReader) GetRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, errors.New("boom")
}

// TestBucketRangeReader_CountsFailedRequests checks that a failed request still increments its stat,
// matching the objstore client's operations_total, which counts every attempt.
func TestBucketRangeReader_CountsFailedRequests(t *testing.T) {
	ctx, capture := xcap.NewCapture(context.Background(), nil)
	ctx, _ = xcap.StartRegion(ctx, "test")
	rr := &bucketRangeReader{bucket: erroringBucketReader{}, path: "obj"}

	_, err := rr.Size(ctx)
	require.Error(t, err)
	_, err = rr.Read(ctx)
	require.Error(t, err)
	_, err = rr.ReadRange(ctx, 0, 10)
	require.Error(t, err)

	require.Equal(t, int64(1), xcap.ValueFromRegion[int64](capture, "test", StatObjectRequestsAttributes))
	require.Equal(t, int64(1), xcap.ValueFromRegion[int64](capture, "test", StatObjectRequestsGet))
	require.Equal(t, int64(1), xcap.ValueFromRegion[int64](capture, "test", StatObjectRequestsGetRange))
}
