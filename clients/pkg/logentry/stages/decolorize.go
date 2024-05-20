package stages

import (
	"github.com/grafana/loki/v3/pkg/logql/log"
)

type decolorizeStage struct{}

func newDecolorizeStage(_ interface{}) (Stage, error) {
	return &decolorizeStage{}, nil
}

// Run implements Stage
func (m *decolorizeStage) Run(in chan Entry) chan Entry {
	decolorizer, _ := log.NewDecolorizer()
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			decolorizedLine, _ := decolorizer.Process(
				e.Timestamp.Unix(),
				[]byte(e.Entry.Line),
				nil,
			)
			e.Entry.Line = string(decolorizedLine)
			out <- e
		}
	}()
	return out
}

// Name implements Stage
func (m *decolorizeStage) Name() string {
	return StageTypeDecolorize
}

// Cleanup implements Stage.
func (*decolorizeStage) Cleanup() {
	// no-op
}
