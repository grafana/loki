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

func (p translateEOFPipeline) Close() {
	p.pipeline.Close()
}

func (p translateEOFPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	rec, err := p.pipeline.Read(ctx)
	return rec, translateEOF(err, false)
}

func translateEOF(err error, toExecutor bool) error {
	if toExecutor {
		// io.EOF to executor.EOF
		if errors.Is(err, io.EOF) {
			err = EOF
		}
	}
	if !toExecutor {
		// executor.EOF to io.EOF
		if errors.Is(err, EOF) {
			err = io.EOF
		}
	}

	return err
}
