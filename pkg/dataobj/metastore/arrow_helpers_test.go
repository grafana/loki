package metastore

import (
	"context"
	"errors"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
)

type sliceRecordBatchReader struct {
	recs []arrow.RecordBatch
	errs []error
	i    int
}

func (r *sliceRecordBatchReader) Read(_ context.Context) (arrow.RecordBatch, error) {
	if r.i >= len(r.recs) {
		return nil, io.EOF
	}
	rec := r.recs[r.i]
	var err error
	if r.errs != nil && r.i < len(r.errs) {
		err = r.errs[r.i]
	}
	r.i++
	if err == nil {
		return rec, nil
	}
	if errors.Is(err, io.EOF) {
		return rec, io.EOF
	}
	return rec, err
}

func (r *sliceRecordBatchReader) Close() {}

var _ ArrowRecordBatchReader = (*sliceRecordBatchReader)(nil)
