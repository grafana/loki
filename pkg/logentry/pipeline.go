package logentry

import (
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/api"
)

// Parser takes an existing set of labels, timestamp and log entry and returns either a possibly mutated
// timestamp and log entry
type Parser interface {
	Parse(labels model.LabelSet, time *time.Time, entry *string)
}

type PipelineStage map[interface{}]interface{}

type Pipeline struct {
	stages []Parser
}

func NewPipeline(stages []PipelineStage) (Pipeline, error) {

	//rg := config.ParserStages[0]["regex"]

	//The idea is to load the stages, possibly using reflection to instantiate based on packages?
	//Then the processor will pass every log line through the pipeline stages
	//With debug logging to show what the labels are before/after as well as timestamp and log message
	//Metrics to cover each pipeline stage and the entire process (do we have to pass the metrics in to the processor??)

	//Error handling, fail on setup errors?, fail on pipeline processing errors?

	//we handle safe casting so we can direct the user to yaml issues if the key isn't a string

	//for _, s := range config.ParserStages {
	//	if len(s) > 1 {
	//		panic("Pipeline stages must contain only one key:")
	//	}
	//
	//	switch s {
	//	case "regex":
	//		var cfg2 Config
	//		err := mapstructure.Decode(rg, &cfg2)
	//		if err != nil {
	//			panic(err)
	//		}
	//	}
	//}
	return Pipeline{}, nil
}

func (p *Pipeline) Process(labels model.LabelSet, time *time.Time, entry *string) {
	//debug log labels, time, and string
	for _, parser := range p.stages {
		parser.Parse(labels, time, entry)
		//debug log labels, time, and string
	}
}

func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		p.Process(labels, &timestamp, &line)
		return next.Handle(labels, timestamp, line)
	})

}
