package log

import (
	"github.com/apache/arrow/go/v14/arrow"
)

type frameSampleExtractor struct {
	Stage
	LineExtractor
	builder *LabelsBuilder
}

func (l *frameSampleExtractor) Process(batch arrow.Record) (arrow.Record, bool) {
	return nil, false
}
