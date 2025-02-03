// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package opentracing

import (
	"context"
	"io"

	"github.com/opentracing/opentracing-go"

	"github.com/thanos-io/objstore"
)

type contextKey struct{}

var tracerKey = contextKey{}

// Tracer interface to provide GetTraceIDFromSpanContext method.
type Tracer interface {
	GetTraceIDFromSpanContext(ctx opentracing.SpanContext) (string, bool)
}

// ContextWithTracer returns a new `context.Context` that holds a reference to given opentracing.Tracer.
func ContextWithTracer(ctx context.Context, tracer opentracing.Tracer) context.Context {
	return context.WithValue(ctx, tracerKey, tracer)
}

// TracerFromContext extracts opentracing.Tracer from the given context.
func TracerFromContext(ctx context.Context) opentracing.Tracer {
	val := ctx.Value(tracerKey)
	if sp, ok := val.(opentracing.Tracer); ok {
		return sp
	}
	return nil
}

// TracingBucket includes bucket operations in the traces.
type TracingBucket struct {
	bkt objstore.Bucket
}

func WrapWithTraces(bkt objstore.Bucket) objstore.InstrumentedBucket {
	return TracingBucket{bkt: bkt}
}

func (t TracingBucket) Provider() objstore.ObjProvider { return t.bkt.Provider() }

func (t TracingBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) (err error) {
	doWithSpan(ctx, "bucket_iter", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("dir", dir)
		err = t.bkt.Iter(spanCtx, dir, f, options...)
	})
	return
}

func (t TracingBucket) IterWithAttributes(ctx context.Context, dir string, f func(attrs objstore.IterObjectAttributes) error, options ...objstore.IterOption) (err error) {
	doWithSpan(ctx, "bucket_iter_with_attrs", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("dir", dir)
		err = t.bkt.IterWithAttributes(spanCtx, dir, f, options...)
	})
	return
}

func (t TracingBucket) SupportedIterOptions() []objstore.IterOptionType {
	return t.bkt.SupportedIterOptions()
}

func (t TracingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	span, spanCtx := startSpan(ctx, "bucket_get")
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
	span, spanCtx := startSpan(ctx, "bucket_getrange")
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
	doWithSpan(ctx, "bucket_exists", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("name", name)
		exists, err = t.bkt.Exists(spanCtx, name)
	})
	return
}

func (t TracingBucket) Attributes(ctx context.Context, name string) (attrs objstore.ObjectAttributes, err error) {
	doWithSpan(ctx, "bucket_attributes", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("name", name)
		attrs, err = t.bkt.Attributes(spanCtx, name)
	})
	return
}

func (t TracingBucket) Upload(ctx context.Context, name string, r io.Reader) (err error) {
	doWithSpan(ctx, "bucket_upload", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("name", name)
		err = t.bkt.Upload(spanCtx, name, r)
	})
	return
}

func (t TracingBucket) GetAndReplace(ctx context.Context, name string, f func(io.Reader) (io.Reader, error)) (err error) {
	doWithSpan(ctx, "bucket_get_and_replace", func(spanCtx context.Context, span opentracing.Span) {
		span.LogKV("name", name)
		err = t.bkt.GetAndReplace(spanCtx, name, f)
	})
	return
}

func (t TracingBucket) Delete(ctx context.Context, name string) (err error) {
	doWithSpan(ctx, "bucket_delete", func(spanCtx context.Context, span opentracing.Span) {
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

func (t TracingBucket) IsAccessDeniedErr(err error) bool {
	return t.bkt.IsAccessDeniedErr(err)
}

func (t TracingBucket) WithExpectedErrs(expectedFunc objstore.IsOpFailureExpectedFunc) objstore.Bucket {
	if ib, ok := t.bkt.(objstore.InstrumentedBucket); ok {
		return TracingBucket{bkt: ib.WithExpectedErrs(expectedFunc)}
	}
	return t
}

func (t TracingBucket) ReaderWithExpectedErrs(expectedFunc objstore.IsOpFailureExpectedFunc) objstore.BucketReader {
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
	objSize, objSizeErr := objstore.TryToGetSize(r)

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

// Aliases to avoid spreading opentracing package to Thanos code.
type Tag = opentracing.Tag
type Tags = opentracing.Tags
type Span = opentracing.Span

// startSpan starts and returns span with `operationName` and hooking as child to a span found within given context if any.
// It uses opentracing.Tracer propagated in context. If no found, it uses noop tracer without notification.
func startSpan(ctx context.Context, operationName string, opts ...opentracing.StartSpanOption) (Span, context.Context) {
	tracer := TracerFromContext(ctx)
	if tracer == nil {
		// No tracing found, return noop span.
		return opentracing.NoopTracer{}.StartSpan(operationName), ctx
	}

	var span Span
	if parentSpan := opentracing.SpanFromContext(ctx); parentSpan != nil {
		opts = append(opts, opentracing.ChildOf(parentSpan.Context()))
	}
	span = tracer.StartSpan(operationName, opts...)
	return span, opentracing.ContextWithSpan(ctx, span)
}

// doWithSpan executes function doFn inside new span with `operationName` name and hooking as child to a span found within given context if any.
// It uses opentracing.Tracer propagated in context. If no found, it uses noop tracer notification.
func doWithSpan(ctx context.Context, operationName string, doFn func(context.Context, Span), _ ...opentracing.StartSpanOption) {
	span, newCtx := startSpan(ctx, operationName)
	defer span.Finish()
	doFn(newCtx, span)
}
