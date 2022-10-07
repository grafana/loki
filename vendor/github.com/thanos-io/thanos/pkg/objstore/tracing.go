// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package objstore

import (
	"context"
	"io"

	"github.com/opentracing/opentracing-go"

	"github.com/thanos-io/thanos/pkg/tracing"
)

// TracingBucket includes bucket operations in the traces.
type TracingBucket struct {
	bkt Bucket
}

func NewTracingBucket(bkt Bucket) InstrumentedBucket {
	return TracingBucket{bkt: bkt}
}

func (t TracingBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...IterOption) (err error) {
	tracing.DoWithSpan(ctx, "bucket_iter", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("dir", dir)
		err = t.bkt.Iter(spanCtx, dir, f, options...)
	})
	return
}

func (t TracingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	span, spanCtx := tracing.StartSpan(ctx, "bucket_get")
	span.LogKV("name", name)

	r, err := t.bkt.Get(spanCtx, name)
	if err != nil {
		span.LogKV("err", err)
		span.Finish()
		return nil, err
	}

	return newTracingReadCloser(r, span), nil
}

func (t TracingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	span, spanCtx := tracing.StartSpan(ctx, "bucket_getrange")
	span.LogKV("name", name, "offset", off, "length", length)

	r, err := t.bkt.GetRange(spanCtx, name, off, length)
	if err != nil {
		span.LogKV("err", err)
		span.Finish()
		return nil, err
	}

	return newTracingReadCloser(r, span), nil
}

func (t TracingBucket) Exists(ctx context.Context, name string) (exists bool, err error) {
	tracing.DoWithSpan(ctx, "bucket_exists", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("name", name)
		exists, err = t.bkt.Exists(spanCtx, name)
	})
	return
}

func (t TracingBucket) Attributes(ctx context.Context, name string) (attrs ObjectAttributes, err error) {
	tracing.DoWithSpan(ctx, "bucket_attributes", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("name", name)
		attrs, err = t.bkt.Attributes(spanCtx, name)
	})
	return
}

func (t TracingBucket) Upload(ctx context.Context, name string, r io.Reader) (err error) {
	tracing.DoWithSpan(ctx, "bucket_upload", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("name", name)
		err = t.bkt.Upload(spanCtx, name, r)
	})
	return
}

func (t TracingBucket) Delete(ctx context.Context, name string) (err error) {
	tracing.DoWithSpan(ctx, "bucket_delete", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("name", name)
		err = t.bkt.Delete(spanCtx, name)
	})
	return
}

func (t TracingBucket) Name() string {
	return "tracing: " + t.bkt.Name()
}

func (t TracingBucket) Close() error {
	return t.bkt.Close()
}

func (t TracingBucket) IsObjNotFoundErr(err error) bool {
	return t.bkt.IsObjNotFoundErr(err)
}

func (t TracingBucket) WithExpectedErrs(expectedFunc IsOpFailureExpectedFunc) Bucket {
	if ib, ok := t.bkt.(InstrumentedBucket); ok {
		return TracingBucket{bkt: ib.WithExpectedErrs(expectedFunc)}
	}
	return t
}

func (t TracingBucket) ReaderWithExpectedErrs(expectedFunc IsOpFailureExpectedFunc) BucketReader {
	return t.WithExpectedErrs(expectedFunc)
}

type tracingReadCloser struct {
	r io.ReadCloser
	s opentracing.Span

	objSize    int64
	objSizeErr error

	read int
}

func newTracingReadCloser(r io.ReadCloser, span opentracing.Span) io.ReadCloser {
	// Since TryToGetSize can only reliably return size before doing any read calls,
	// we call during "construction" and remember the results.
	objSize, objSizeErr := TryToGetSize(r)

	return &tracingReadCloser{r: r, s: span, objSize: objSize, objSizeErr: objSizeErr}
}

func (t *tracingReadCloser) ObjectSize() (int64, error) {
	return t.objSize, t.objSizeErr
}

func (t *tracingReadCloser) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.read += n
	}
	if err != nil && err != io.EOF && t.s != nil {
		t.s.LogKV("err", err)
	}
	return n, err
}

func (t *tracingReadCloser) Close() error {
	err := t.r.Close()
	if t.s != nil {
		t.s.LogKV("read", t.read)
		if err != nil {
			t.s.LogKV("close err", err)
		}
		t.s.Finish()
		t.s = nil
	}
	return err
}
