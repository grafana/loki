package store

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func TestBucketReaderAt(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	data := []byte("hello, world! this is test data for range reads")
	require.NoError(t, bucket.Upload(context.Background(), "test/object", bytes.NewReader(data)))

	reader := NewBucketReaderAt(context.Background(), bucket, "test/object")

	// Read from the start.
	buf := make([]byte, 5)
	n, err := reader.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, "hello", string(buf))

	// Read from the middle.
	buf = make([]byte, 5)
	n, err = reader.ReadAt(buf, 7)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, "world", string(buf))

	// Empty read.
	buf = make([]byte, 0)
	n, err = reader.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}
