package executor

import (
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/pkg/errors"
)

func TranslateEOF(pipeline Pipeline) Pipeline {
	return translateEOFPipeline{pipeline, false}
}

type translateEOFPipeline struct {
	pipeline   Pipeline
	toInternal bool
}

func (p translateEOFPipeline) Close() {
	p.pipeline.Close()
}

func (p translateEOFPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	rec, err := p.pipeline.Read(ctx)
	return rec, translateEOF(err, p.toInternal)
}

func translateEOF(err error, toInternal bool) error {
	if toInternal {
		// io.EOF to executor.EOF
		if errors.Is(err, io.EOF) {
			err = EOF
		}
	}
	if !toInternal {
		// executor.EOF to EOF
		if errors.Is(err, EOF) {
			err = io.EOF
		}
	}

	return err
}
