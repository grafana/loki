package metastore

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/xcap"
)

type ArrowRecordBatchReader interface {
	Open(ctx context.Context) error
	Read(ctx context.Context) (arrow.RecordBatch, error)
	Close()
}

type xcapArrowRecordBatchReader struct {
	r            ArrowRecordBatchReader
	readSpan     *xcap.Span
	openSpanName string
	readSpanName string
}

func newXcapArrowRecordBatchReader(r ArrowRecordBatchReader, spanNamePrefix string) *xcapArrowRecordBatchReader {
	return &xcapArrowRecordBatchReader{
		r:            r,
		readSpan:     nil,
		openSpanName: fmt.Sprintf("%s.Open", spanNamePrefix),
		readSpanName: fmt.Sprintf("%s.Read", spanNamePrefix),
	}
}

func (r *xcapArrowRecordBatchReader) Open(ctx context.Context) error {
	ctx, sp := xcap.StartSpan(ctx, tracer, r.openSpanName)
	defer sp.End()

	return r.r.Open(ctx)
}

func (r *xcapArrowRecordBatchReader) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if r.readSpan == nil {
		ctx, r.readSpan = xcap.StartSpan(ctx, tracer, r.readSpanName)
	} else {
		ctx = xcap.ContextWithSpan(ctx, r.readSpan)
	}

	return r.r.Read(ctx)
}

func (r *xcapArrowRecordBatchReader) Close() {
	r.r.Close()

	if r.readSpan != nil {
		r.readSpan.End()
	}
}

var (
	_ ArrowRecordBatchReader = (*xcapArrowRecordBatchReader)(nil)
)
