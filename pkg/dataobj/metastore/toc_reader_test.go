package metastore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
)

// byteCountingBucket counts the bytes the readers of Get and GetRange return.
type byteCountingBucket struct {
	objstore.Bucket
	bytes atomic.Int64
	gets  atomic.Int64
}

type byteCountingReader struct {
	io.ReadCloser
	bytes *atomic.Int64
}

func (r *byteCountingReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytes.Add(int64(n))
	return n, err
}

func (b *byteCountingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	b.gets.Add(1)
	rc, err := b.Bucket.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return &byteCountingReader{ReadCloser: rc, bytes: &b.bytes}, nil
}

func (b *byteCountingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	b.gets.Add(1)
	rc, err := b.Bucket.GetRange(ctx, name, off, length)
	if err != nil {
		return nil, err
	}
	return &byteCountingReader{ReadCloser: rc, bytes: &b.bytes}, nil
}

var errFailingNotFound = errors.New("not found")

// notFoundTestBucket fails every read with err. If exactNotFound is set, it
// reports as not found only an error equal to err, like a bucket that does
// not unwrap.
type notFoundTestBucket struct {
	objstore.Bucket
	err           error
	exactNotFound bool
}

func (b *notFoundTestBucket) Get(context.Context, string) (io.ReadCloser, error) { return nil, b.err }

func (b *notFoundTestBucket) GetRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, b.err
}

func (b *notFoundTestBucket) Attributes(context.Context, string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, fmt.Errorf("reading attributes: %w", b.err)
}

func (b *notFoundTestBucket) IsObjNotFoundErr(err error) bool {
	return b.exactNotFound && err == b.err
}

func TestReadWatermarks(t *testing.T) {
	ctx := context.Background()

	t.Run("returns no watermarks and no error when the ToC does not exist", func(t *testing.T) {
		rows, err := ReadWatermarks(ctx, objstore.NewInMemBucket(), wmTenant, unixTime(0))

		require.NoError(t, err)
		require.Empty(t, rows)
	})

	t.Run("returns no watermarks when the ToC holds only index pointers", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a", "indexes/b"}, nil)

		rows, err := ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))

		require.NoError(t, err)
		require.Empty(t, rows)
	})

	t.Run("returns the watermarks of several topics and partitions", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		want := []watermarks.Watermark{
			{Topic: "a", Partition: 0, Watermark: 10},
			{Topic: "a", Partition: 1, Watermark: 20},
			{Topic: "b", Partition: 0, Watermark: 30},
			{Topic: "b", Partition: 7, Watermark: 0},
		}
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, want)

		rows, err := ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))

		require.NoError(t, err)
		require.ElementsMatch(t, want, rows)
	})

	t.Run("truncates a window that is not aligned to the window size", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 5)})

		rows, err := ReadWatermarks(ctx, bucket, wmTenant, unixTime(0).Add(MetastoreWindowSize-1))

		require.NoError(t, err)
		require.Equal(t, []watermarks.Watermark{wm(0, 5)}, rows)
	})

	t.Run("returns an unrecoverable error when the ToC holds another tenant", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 5)})
		raw := readRawToC(ctx, t, bucket, wmTocPath())
		otherPath := TableOfContentsPath("other", unixTime(0))
		require.NoError(t, bucket.Upload(ctx, otherPath, bytes.NewReader(raw)))

		rows, err := ReadWatermarks(ctx, bucket, "other", unixTime(0))

		require.ErrorIs(t, err, errUnrecoverable)
		require.Empty(t, rows)
	})

	t.Run("round trips with the writer and returns the latest value after each raise", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 99, nil))
		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 1, 50, nil))
		rows, err := ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))
		require.NoError(t, err)
		require.ElementsMatch(t, []watermarks.Watermark{wm(0, 99), wm(1, 50)}, rows)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/c", 0, 199, int64Ptr(99)))
		rows, err = ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))
		require.NoError(t, err)
		require.ElementsMatch(t, []watermarks.Watermark{wm(0, 199), wm(1, 50)}, rows)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/d", 0, 299, int64Ptr(199)))
		rows, err = ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))
		require.NoError(t, err)
		require.ElementsMatch(t, []watermarks.Watermark{wm(0, 299), wm(1, 50)}, rows)
	})

	t.Run("returns the error when the bucket fails with an error other than not found", func(t *testing.T) {
		errBoom := errors.New("boom")
		bucket := &notFoundTestBucket{Bucket: objstore.NewInMemBucket(), err: errBoom}

		rows, err := ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))

		require.ErrorIs(t, err, errBoom)
		require.Nil(t, rows)
	})

	t.Run("returns no watermarks when the bucket matches only the unwrapped not-found error", func(t *testing.T) {
		bucket := &notFoundTestBucket{Bucket: objstore.NewInMemBucket(), err: errFailingNotFound, exactNotFound: true}

		rows, err := ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))

		require.NoError(t, err)
		require.Nil(t, rows)
	})

	t.Run("reads far fewer bytes than the ToC holds", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		paths := make([]string, 50000)
		for i := range paths {
			paths[i] = fmt.Sprintf("indexes/%x", sha256.Sum256([]byte(strconv.Itoa(i))))
		}
		seedWatermarkToC(t, inner, paths, []watermarks.Watermark{wm(0, 99), wm(1, 98)})
		attrs, err := inner.Attributes(ctx, wmTocPath())
		require.NoError(t, err)
		bucket := &byteCountingBucket{Bucket: inner}

		rows, err := ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))

		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.Less(t, bucket.bytes.Load(), attrs.Size/10)
		require.LessOrEqual(t, bucket.gets.Load(), int64(3))
	})
}

func TestReadWatermark(t *testing.T) {
	ctx := context.Background()

	bucket := objstore.NewInMemBucket()
	seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 10), wm(1, 0)})

	t.Run("returns the value of the topic and partition", func(t *testing.T) {
		got, err := ReadWatermark(ctx, bucket, wmTenant, unixTime(0), wmTopic, 0)

		require.NoError(t, err)
		require.Equal(t, int64Ptr(10), got)
	})

	t.Run("returns a pointer to zero when the watermark is zero", func(t *testing.T) {
		got, err := ReadWatermark(ctx, bucket, wmTenant, unixTime(0), wmTopic, 1)

		require.NoError(t, err)
		require.Equal(t, int64Ptr(0), got)
	})

	t.Run("returns nil when the ToC has no row for the partition", func(t *testing.T) {
		got, err := ReadWatermark(ctx, bucket, wmTenant, unixTime(0), wmTopic, 2)

		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("returns nil when the topic differs", func(t *testing.T) {
		got, err := ReadWatermark(ctx, bucket, wmTenant, unixTime(0), "other", 0)

		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("returns the error and no value when the ToC holds another tenant", func(t *testing.T) {
		raw := readRawToC(ctx, t, bucket, wmTocPath())
		other := objstore.NewInMemBucket()
		require.NoError(t, other.Upload(ctx, TableOfContentsPath("other", unixTime(0)), bytes.NewReader(raw)))

		got, err := ReadWatermark(ctx, other, "other", unixTime(0), wmTopic, 0)

		require.ErrorIs(t, err, errUnrecoverable)
		require.Nil(t, got)
	})

	t.Run("returns nil when the ToC does not exist", func(t *testing.T) {
		got, err := ReadWatermark(ctx, bucket, wmTenant, unixTime(0).Add(MetastoreWindowSize), wmTopic, 0)

		require.NoError(t, err)
		require.Nil(t, got)
	})
}
