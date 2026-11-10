package dataobj_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const instrumentedTestRegion = "test"

// callCountingBucket counts the calls that reach the wrapped bucket, to compare with the statistics.
type callCountingBucket struct {
	objstore.Bucket
	attributes atomic.Int64
	gets       atomic.Int64
	getRanges  atomic.Int64
}

func (b *callCountingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	b.attributes.Inc()
	return b.Bucket.Attributes(ctx, name)
}

func (b *callCountingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	b.gets.Inc()
	return b.Bucket.Get(ctx, name)
}

func (b *callCountingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	b.getRanges.Inc()
	return b.Bucket.GetRange(ctx, name, off, length)
}

// erroringBucket fails every read with err, which is not a not-found error.
type erroringBucket struct {
	objstore.Bucket
	err error
}

func (b *erroringBucket) Attributes(context.Context, string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, b.err
}

func (b *erroringBucket) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, b.err
}

func (b *erroringBucket) GetRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, b.err
}

// faultyBodyBucket serves bodies that deliver three bytes and then fail with err on every read.
type faultyBodyBucket struct {
	objstore.Bucket
	err error
}

func (b *faultyBodyBucket) Get(context.Context, string) (io.ReadCloser, error) {
	return &faultyBody{data: []byte("abc"), err: b.err}, nil
}

func (b *faultyBodyBucket) GetRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return &faultyBody{data: []byte("abc"), err: b.err}, nil
}

type faultyBody struct {
	data []byte
	err  error
}

func (b *faultyBody) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, b.err
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func (b *faultyBody) Close() error { return nil }

// observingBucket remembers how many GetRange requests the context's region held when GetRange ran.
type observingBucket struct {
	objstore.Bucket
	requestsSeen int64
}

func (b *observingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	b.requestsSeen, _ = xcap.RegionFromContext(ctx).ObservationForStatistic(dataobj.StatObjectRequestsGetRange).Int64()
	return b.Bucket.GetRange(ctx, name, off, length)
}

func TestNewInstrumentedBucketReader(t *testing.T) {
	content := []byte("0123456789")

	// newCapturedContext returns a context whose region the statistics are recorded into, and a func
	// that reads one statistic back from that region.
	newCapturedContext := func(t *testing.T) (context.Context, func(xcap.Statistic) int64) {
		t.Helper()
		ctx, capture := xcap.NewCapture(t.Context(), nil)
		ctx, _ = xcap.StartRegion(ctx, instrumentedTestRegion)
		return ctx, func(stat xcap.Statistic) int64 {
			return xcap.ValueFromRegion[int64](capture, instrumentedTestRegion, stat)
		}
	}

	newBucket := func(t *testing.T) objstore.Bucket {
		t.Helper()
		bucket := objstore.NewInMemBucket()
		require.NoError(t, bucket.Upload(t.Context(), "obj", bytes.NewReader(content)))
		return bucket
	}

	t.Run("a successful Attributes call records one request and no failure", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		reader := dataobj.NewInstrumentedBucketReader(newBucket(t))

		attrs, err := reader.Attributes(ctx, "obj")
		require.NoError(t, err)
		require.Equal(t, int64(len(content)), attrs.Size)

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsAttributes))
		require.Zero(t, value(dataobj.StatObjectRequestFailuresAttributes))
	})

	t.Run("a successful Get call records one request and no failure, and the bytes once the body closes", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		reader := dataobj.NewInstrumentedBucketReader(newBucket(t))

		rc, err := reader.Get(ctx, "obj")
		require.NoError(t, err)
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, content, got)

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGet))
		require.Zero(t, value(dataobj.StatObjectRequestFailuresGet))
		require.Zero(t, value(dataobj.StatObjectBytesDownloaded), "the bytes are recorded on close, not while reading")

		require.NoError(t, rc.Close())
		require.Equal(t, int64(len(content)), value(dataobj.StatObjectBytesDownloaded))
	})

	t.Run("a successful GetRange call records one request and no failure, and the bytes read", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		reader := dataobj.NewInstrumentedBucketReader(newBucket(t))

		rc, err := reader.GetRange(ctx, "obj", 2, 4)
		require.NoError(t, err)
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, content[2:6], got)
		require.NoError(t, rc.Close())

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGetRange))
		require.Zero(t, value(dataobj.StatObjectRequestFailuresGetRange))
		require.Equal(t, int64(4), value(dataobj.StatObjectBytesDownloaded))
	})

	t.Run("a call for an object that does not exist records one request and no failure", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		bucket := newBucket(t)
		reader := dataobj.NewInstrumentedBucketReader(bucket)

		_, err := reader.Attributes(ctx, "missing")
		require.True(t, bucket.IsObjNotFoundErr(err), "the backend error reaches the caller unchanged")
		_, err = reader.Get(ctx, "missing")
		require.True(t, bucket.IsObjNotFoundErr(err), "the backend error reaches the caller unchanged")
		_, err = reader.GetRange(ctx, "missing", 0, 1)
		require.True(t, bucket.IsObjNotFoundErr(err), "the backend error reaches the caller unchanged")

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsAttributes))
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGet))
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGetRange))
		require.Zero(t, value(dataobj.StatObjectRequestFailuresAttributes))
		require.Zero(t, value(dataobj.StatObjectRequestFailuresGet))
		require.Zero(t, value(dataobj.StatObjectRequestFailuresGetRange))
		require.Zero(t, value(dataobj.StatObjectBytesDownloaded))
	})

	t.Run("a call that fails with another error records one request and one failure of its own operation", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		wantErr := fmt.Errorf("Get %q: %w", "obj", io.EOF)
		reader := dataobj.NewInstrumentedBucketReader(&erroringBucket{Bucket: newBucket(t), err: wantErr})

		_, err := reader.Attributes(ctx, "obj")
		require.ErrorIs(t, err, wantErr)
		_, err = reader.Get(ctx, "obj")
		require.ErrorIs(t, err, wantErr)
		_, err = reader.GetRange(ctx, "obj", 0, 1)
		require.ErrorIs(t, err, wantErr)

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsAttributes))
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestFailuresAttributes))
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGet))
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestFailuresGet))
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGetRange))
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestFailuresGetRange))
		require.Zero(t, value(dataobj.StatObjectBytesDownloaded))
	})

	t.Run("it records the request before the object store receives it", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		bucket := &observingBucket{Bucket: newBucket(t)}
		reader := dataobj.NewInstrumentedBucketReader(bucket)

		rc, err := reader.GetRange(ctx, "obj", 0, 1)
		require.NoError(t, err)
		require.NoError(t, rc.Close())

		require.Equal(t, int64(1), bucket.requestsSeen)
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGetRange))
	})

	for _, tc := range []struct {
		name     string
		request  xcap.Statistic
		failures xcap.Statistic
		open     func(ctx context.Context, reader objstore.BucketReader) (io.ReadCloser, error)
	}{
		{
			name:     "Get",
			request:  dataobj.StatObjectRequestsGet,
			failures: dataobj.StatObjectRequestFailuresGet,
			open: func(ctx context.Context, reader objstore.BucketReader) (io.ReadCloser, error) {
				return reader.Get(ctx, "obj")
			},
		},
		{
			name:     "GetRange",
			request:  dataobj.StatObjectRequestsGetRange,
			failures: dataobj.StatObjectRequestFailuresGetRange,
			open: func(ctx context.Context, reader objstore.BucketReader) (io.ReadCloser, error) {
				return reader.GetRange(ctx, "obj", 0, 10)
			},
		},
	} {
		t.Run("a "+tc.name+" body that fails with an unexpected error records one failure of its request", func(t *testing.T) {
			ctx, value := newCapturedContext(t)
			wantErr := errors.New("connection reset by peer")
			reader := dataobj.NewInstrumentedBucketReader(&faultyBodyBucket{Bucket: newBucket(t), err: wantErr})

			rc, err := tc.open(ctx, reader)
			require.NoError(t, err)
			_, err = io.ReadAll(rc)
			require.ErrorIs(t, err, wantErr)
			require.NoError(t, rc.Close())

			require.Equal(t, int64(1), value(tc.request))
			require.Equal(t, int64(1), value(tc.failures))
			require.Equal(t, int64(3), value(dataobj.StatObjectBytesDownloaded), "the bytes delivered before the failure still count")
		})
	}

	t.Run("a body that ends early with an unexpected EOF records a failure", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		reader := dataobj.NewInstrumentedBucketReader(&faultyBodyBucket{Bucket: newBucket(t), err: io.ErrUnexpectedEOF})

		rc, err := reader.Get(ctx, "obj")
		require.NoError(t, err)
		_, err = io.ReadAll(rc)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestFailuresGet))
	})

	t.Run("a body that keeps failing records one failure", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		reader := dataobj.NewInstrumentedBucketReader(&faultyBodyBucket{Bucket: newBucket(t), err: errors.New("connection reset by peer")})

		rc, err := reader.Get(ctx, "obj")
		require.NoError(t, err)
		buf := make([]byte, 8)
		for range 5 {
			_, _ = rc.Read(buf)
		}

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGet))
		require.Equal(t, int64(1), value(dataobj.StatObjectRequestFailuresGet))
	})

	t.Run("a body that fails after the caller canceled its context records no failure", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		ctx, cancel := context.WithCancel(ctx)
		reader := dataobj.NewInstrumentedBucketReader(&faultyBodyBucket{Bucket: newBucket(t), err: fmt.Errorf("read tcp: %w", context.Canceled)})

		rc, err := reader.Get(ctx, "obj")
		require.NoError(t, err)
		cancel()
		_, err = io.ReadAll(rc)
		require.Error(t, err)

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGet), "a canceled request is still a request")
		require.Zero(t, value(dataobj.StatObjectRequestFailuresGet))
	})

	t.Run("a call that fails with a real error after the context is done records a failure", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		reader := dataobj.NewInstrumentedBucketReader(&erroringBucket{Bucket: newBucket(t), err: errors.New("503 service unavailable")})

		_, err := reader.Get(ctx, "obj")
		require.Error(t, err)

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestFailuresGet))
	})

	t.Run("a call that fails after the caller canceled its context records no failure", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		reader := dataobj.NewInstrumentedBucketReader(&erroringBucket{Bucket: newBucket(t), err: context.Canceled})

		_, err := reader.Get(ctx, "obj")
		require.ErrorIs(t, err, context.Canceled)

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsGet))
		require.Zero(t, value(dataobj.StatObjectRequestFailuresGet))
	})

	t.Run("a body that fails with a not-found error records no failure", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		bucket := newBucket(t)
		_, notFound := bucket.Get(ctx, "missing")
		require.True(t, bucket.IsObjNotFoundErr(notFound))
		reader := dataobj.NewInstrumentedBucketReader(&faultyBodyBucket{Bucket: bucket, err: notFound})

		rc, err := reader.Get(ctx, "obj")
		require.NoError(t, err)
		_, err = io.ReadAll(rc)
		require.Error(t, err)

		require.Zero(t, value(dataobj.StatObjectRequestFailuresGet))
	})

	t.Run("a body closed before its end records no failure and the bytes read", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		reader := dataobj.NewInstrumentedBucketReader(newBucket(t))

		rc, err := reader.Get(ctx, "obj")
		require.NoError(t, err)
		_, err = io.ReadFull(rc, make([]byte, 2))
		require.NoError(t, err)
		require.NoError(t, rc.Close())

		require.Zero(t, value(dataobj.StatObjectRequestFailuresGet))
		require.Equal(t, int64(2), value(dataobj.StatObjectBytesDownloaded))
	})

	t.Run("a call without a region in the context records nothing and does not panic", func(t *testing.T) {
		reader := dataobj.NewInstrumentedBucketReader(newBucket(t))

		require.NotPanics(t, func() {
			_, _ = reader.Attributes(t.Context(), "obj")
			rc, err := reader.Get(t.Context(), "obj")
			require.NoError(t, err)
			require.NoError(t, rc.Close())
			_, _ = reader.GetRange(t.Context(), "missing", 0, 1)
		})
	})

	t.Run("wrapping an instrumented reader returns the same reader", func(t *testing.T) {
		once := dataobj.NewInstrumentedBucketReader(newBucket(t))
		require.Same(t, once, dataobj.NewInstrumentedBucketReader(once))
	})

	t.Run("a call through a reader wrapped twice records one request", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		reader := dataobj.NewInstrumentedBucketReader(dataobj.NewInstrumentedBucketReader(newBucket(t)))

		_, err := reader.Attributes(ctx, "obj")
		require.NoError(t, err)

		require.Equal(t, int64(1), value(dataobj.StatObjectRequestsAttributes))
	})

	t.Run("other methods pass through and record nothing", func(t *testing.T) {
		ctx, value := newCapturedContext(t)
		reader := dataobj.NewInstrumentedBucketReader(newBucket(t))

		exists, err := reader.Exists(ctx, "obj")
		require.NoError(t, err)
		require.True(t, exists)

		require.Zero(t, value(dataobj.StatObjectRequestsAttributes))
		require.Zero(t, value(dataobj.StatObjectRequestsGet))
		require.Zero(t, value(dataobj.StatObjectRequestsGetRange))
	})
}

func TestFromBucket_RequestStatistics(t *testing.T) {
	raw := buildObject(t,
		sectionSpec{typ: logsSectionType, meta: []byte("logs-meta"), data: []byte("logs-data")},
		sectionSpec{typ: streamsSectionType, meta: []byte("streams-meta"), data: []byte("streams-data")},
	)

	newBucket := func(t *testing.T) *callCountingBucket {
		t.Helper()
		bucket := objstore.NewInMemBucket()
		require.NoError(t, bucket.Upload(t.Context(), "obj", bytes.NewReader(raw)))
		return &callCountingBucket{Bucket: bucket}
	}

	// readEverything opens the object and reads every section, so the open and the reads all issue
	// requests that reach the bucket.
	readEverything := func(ctx context.Context, t *testing.T, bucket objstore.BucketReader) {
		t.Helper()
		obj, err := dataobj.FromBucket(ctx, bucket, "obj", 0)
		require.NoError(t, err)
		for _, sec := range obj.Sections() {
			readAll(t, func() (io.ReadCloser, error) { return sec.Reader.DataRange(ctx, 0, sec.Reader.DataSize()) })
		}
	}

	requestStats := func(capture *xcap.Capture) map[string]int64 {
		return map[string]int64{
			"attributes": xcap.ValueFromRegion[int64](capture, instrumentedTestRegion, dataobj.StatObjectRequestsAttributes),
			"get":        xcap.ValueFromRegion[int64](capture, instrumentedTestRegion, dataobj.StatObjectRequestsGet),
			"get_range":  xcap.ValueFromRegion[int64](capture, instrumentedTestRegion, dataobj.StatObjectRequestsGetRange),
		}
	}

	t.Run("it records every request the bucket serves, for a bucket that is not instrumented", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(t.Context(), nil)
		ctx, _ = xcap.StartRegion(ctx, instrumentedTestRegion)
		bucket := newBucket(t)

		readEverything(ctx, t, bucket)

		require.Positive(t, bucket.getRanges.Load(), "the fixture must issue range reads for the comparison to mean anything")
		require.Equal(t, map[string]int64{
			"attributes": bucket.attributes.Load(),
			"get":        bucket.gets.Load(),
			"get_range":  bucket.getRanges.Load(),
		}, requestStats(capture))
	})

	t.Run("it records each request once, for a bucket that is already instrumented", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(t.Context(), nil)
		ctx, _ = xcap.StartRegion(ctx, instrumentedTestRegion)
		bucket := newBucket(t)

		readEverything(ctx, t, dataobj.NewInstrumentedBucketReader(bucket))

		require.Equal(t, map[string]int64{
			"attributes": bucket.attributes.Load(),
			"get":        bucket.gets.Load(),
			"get_range":  bucket.getRanges.Load(),
		}, requestStats(capture))
	})

	t.Run("it records the bytes the bucket delivered", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(t.Context(), nil)
		ctx, _ = xcap.StartRegion(ctx, instrumentedTestRegion)

		readEverything(ctx, t, newBucket(t))

		require.Positive(t, xcap.ValueFromRegion[int64](capture, instrumentedTestRegion, dataobj.StatObjectBytesDownloaded))
	})
}
