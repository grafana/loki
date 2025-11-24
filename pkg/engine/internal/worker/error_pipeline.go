package worker

import (
	"context"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
)

type errorPipeline []error

var _ executor.Pipeline = errorPipeline(nil)

func (ep errorPipeline) Read(_ context.Context) (arrow.RecordBatch, error) {
	return nil, errors.Join(ep...)
}

func (ep errorPipeline) Close() {}
