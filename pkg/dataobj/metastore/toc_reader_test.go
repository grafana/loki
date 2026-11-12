package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
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

	t.Run("returns the watermarks of the version it read when the ToC changes after the read", func(t *testing.T) {
		const watermarksPastFirstRangeRead = 20000
		before := make([]watermarks.Watermark, watermarksPastFirstRangeRead)
		after := make([]watermarks.Watermark, len(before))
		for i := range before {
			before[i] = wm(int32(i), int64(i))
			after[i] = wm(int32(i), int64(i)+1)
		}
		bucket := &rewritingBucket{Bucket: objstore.NewInMemBucket()}
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, before)
		bucket.afterGet = func() {
			seedWatermarkToC(t, bucket.Bucket, []string{"indexes/a"}, after)
		}

		rows, err := ReadWatermarks(ctx, bucket, wmTenant, unixTime(0))

		require.NoError(t, err)
		require.ElementsMatch(t, before, rows)
	})
}

// rewritingBucket calls afterGet once, after the first Get or GetRange returns.
type rewritingBucket struct {
	objstore.Bucket
	afterGet func()
}

func (b *rewritingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	rc, err := b.Bucket.Get(ctx, name)
	b.runAfterGet()
	return rc, err
}

func (b *rewritingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	rc, err := b.Bucket.GetRange(ctx, name, off, length)
	b.runAfterGet()
	return rc, err
}

func (b *rewritingBucket) runAfterGet() {
	if b.afterGet != nil {
		afterGet := b.afterGet
		b.afterGet = nil
		afterGet()
	}
}

// oversizeBucket serves every Get with a reader that reports a size over maxTableOfContentsSize and fails every read.
type oversizeBucket struct {
	objstore.Bucket
}

func (b *oversizeBucket) Get(context.Context, string) (io.ReadCloser, error) {
	return objstore.ObjectSizerReadCloser{
		ReadCloser: io.NopCloser(iotest.ErrReader(errors.New("unexpected read"))),
		Size:       func() (int64, error) { return maxTableOfContentsSize + 1, nil },
	}, nil
}

func TestOpenTableOfContents(t *testing.T) {
	path := TableOfContentsPath("tenant", unixTime(0))

	t.Run("returns the pointers of the version it read when the ToC changes after the open", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		uploadToC(t, bucket, path, "tenant", "index-a")

		toc, err := OpenTableOfContents(t.Context(), bucket, path)
		require.NoError(t, err)

		uploadToC(t, bucket, path, "tenant", "index-b", "index-c")

		var paths []string
		require.NoError(t, forEachTocPointer(t.Context(), toc, func(pointer indexpointers.IndexPointer) error {
			paths = append(paths, pointer.Path)
			return nil
		}))
		require.Equal(t, []string{"index-a"}, paths)
	})

	t.Run("reads the whole ToC with one request", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		uploadToC(t, inner, path, "tenant", "index-a")
		attrs, err := inner.Attributes(t.Context(), path)
		require.NoError(t, err)
		bucket := &byteCountingBucket{Bucket: inner}

		_, err = OpenTableOfContents(t.Context(), bucket, path)

		require.NoError(t, err)
		require.Equal(t, int64(1), bucket.gets.Load())
		require.Equal(t, attrs.Size, bucket.bytes.Load())
	})

	t.Run("returns an error that the bucket reports as not found when the ToC does not exist", func(t *testing.T) {
		bucket := &notFoundTestBucket{Bucket: objstore.NewInMemBucket(), err: errFailingNotFound, exactNotFound: true}

		_, err := OpenTableOfContents(t.Context(), bucket, path)

		require.True(t, bucket.IsObjNotFoundErr(err))
	})

	t.Run("returns an error that is not a not-found error when the ToC is empty", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		require.NoError(t, bucket.Upload(t.Context(), path, bytes.NewReader(nil)))

		_, err := OpenTableOfContents(t.Context(), bucket, path)

		require.EqualError(t, err, "metastore object "+path+" is empty")
		require.False(t, bucket.IsObjNotFoundErr(err))
	})

	t.Run("returns an error without a read when the bucket reports a ToC over the size limit", func(t *testing.T) {
		bucket := &oversizeBucket{Bucket: objstore.NewInMemBucket()}

		_, err := OpenTableOfContents(t.Context(), bucket, path)

		require.ErrorContains(t, err, fmt.Sprintf("more than the limit of %d bytes", maxTableOfContentsSize))
		require.False(t, bucket.IsObjNotFoundErr(err))
	})

	t.Run("returns an error when the ToC is not a data object", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		require.NoError(t, bucket.Upload(t.Context(), path, bytes.NewReader([]byte("not a data object"))))

		_, err := OpenTableOfContents(t.Context(), bucket, path)

		require.ErrorContains(t, err, "decoding ToC "+path)
	})
}

func TestReadAtMost(t *testing.T) {
	data := []byte("0123456789")

	t.Run("returns all bytes when the size is unknown", func(t *testing.T) {
		got, err := readAtMost(bytes.NewReader(data), -1, 100)

		require.NoError(t, err)
		require.Equal(t, data, got)
	})

	t.Run("returns all bytes when the size is known", func(t *testing.T) {
		got, err := readAtMost(iotest.OneByteReader(bytes.NewReader(data)), int64(len(data)), 100)

		require.NoError(t, err)
		require.Equal(t, data, got)
	})

	t.Run("allocates only the buffer and the extra byte when the size is known", func(t *testing.T) {
		large := bytes.Repeat([]byte("x"), 1<<20)
		reader := bytes.NewReader(large)
		readAllocs := func(size int64) float64 {
			return testing.AllocsPerRun(10, func() {
				reader.Reset(large)
				_, err := readAtMost(reader, size, int64(len(large)))
				require.NoError(t, err)
			})
		}

		require.Less(t, readAllocs(int64(len(large))), readAllocs(-1))
		require.LessOrEqual(t, readAllocs(int64(len(large))), 2.0, "the buffer and the extra byte")
	})

	t.Run("returns an error when the reader holds more bytes than the known size", func(t *testing.T) {
		_, err := readAtMost(bytes.NewReader(data), 2, 100)

		require.EqualError(t, err, "the reader has more than its size of 2 bytes")
	})

	t.Run("returns an error when the reader holds fewer bytes than the known size", func(t *testing.T) {
		_, err := readAtMost(bytes.NewReader(data), int64(len(data))+1, 100)

		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("returns the error of the reader when it fails after the known size", func(t *testing.T) {
		errRead := errors.New("read failed")

		_, err := readAtMost(io.MultiReader(bytes.NewReader(data), iotest.ErrReader(errRead)), int64(len(data)), 100)

		require.ErrorIs(t, err, errRead)
	})

	t.Run("returns all bytes when the reader holds exactly the limit", func(t *testing.T) {
		got, err := readAtMost(bytes.NewReader(data), -1, int64(len(data)))

		require.NoError(t, err)
		require.Equal(t, data, got)
	})

	t.Run("returns an error when the reader holds more bytes than the limit", func(t *testing.T) {
		_, err := readAtMost(bytes.NewReader(data), -1, int64(len(data))-1)

		require.EqualError(t, err, "the reader has more than the limit of 9 bytes")
	})

	t.Run("returns an error without a read when the known size is over the limit", func(t *testing.T) {
		_, err := readAtMost(iotest.ErrReader(errors.New("unexpected read")), 11, 10)

		require.EqualError(t, err, "the reader has 11 bytes, more than the limit of 10 bytes")
	})

	t.Run("returns the error of the reader", func(t *testing.T) {
		errRead := errors.New("read failed")

		_, err := readAtMost(iotest.ErrReader(errRead), -1, 10)

		require.ErrorIs(t, err, errRead)
	})
}

func TestIsTableOfContentsPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{name: "accepts a ToC path relative to the index bucket", path: TableOfContentsPath("tenant", unixTime(0)), want: true},
		{name: "accepts a ToC path that starts with the index storage prefix", path: IndexStoragePrefix + "/" + TableOfContentsPath("tenant", unixTime(0)), want: true},
		{name: "rejects an index object path", path: IndexStoragePrefix + "/indexes/ab/cdef", want: false},
		{name: "rejects a logs object path", path: "objects/ab/cdef", want: false},
		{name: "rejects a file under the ToC prefix with another name", path: TableOfContentsWindowPrefix(unixTime(0)) + "tenant/other", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsTableOfContentsPath(tc.path))
		})
	}
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
