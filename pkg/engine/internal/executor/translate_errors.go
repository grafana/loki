package executor

import (
	"context"
	"errors"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
)

func TranslateEOF(pipeline Pipeline) Pipeline {
	return translateEOFPipeline{pipeline}
}

type translateEOFPipeline struct {
	pipeline Pipeline
}

func (p translateEOFPipeline) Open(ctx context.Context) error {
	return p.pipeline.Open(ctx)
}

func (p translateEOFPipeline) Close() {
	p.pipeline.Close()
}

// Read reads from the wrapped pipeline and turns [EOF] into [io.EOF].
func (p translateEOFPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	rec, err := p.pipeline.Read(ctx)
	if errors.Is(err, EOF) {
		err = io.EOF
	}
	return rec, err
}
