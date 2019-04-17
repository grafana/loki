package entry

import (
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/parser/entry/parsers"
)

type Config struct {
	//FIXME do we keep the yaml the same? we have to accommodate the kube label parsing happening first (so we can act on those labels)
	ParserStages []map[interface{}]interface{} `yaml:"parser_stages"`
}

type Processor struct {
	parsers []Parser
}

func NewProcessor(config Config) (Processor, error) {

	rg := config.ParserStages[0]["regex"]

	//The idea is to load the stages, possibly using reflection to instantiate based on packages?
	//Then the processor will pass every log line through the pipeline stages
	//With debug logging to show what the labels are before/after as well as timestamp and log message
	//Metrics to cover each pipeline stage and the entire process (do we have to pass the metrics in to the processor??)

	//Error handling, fail on setup errors?, fail on pipeline processing errors?

	//we handle safe casting so we can direct the user to yaml issues if the key isn't a string

	for _, s := range config.ParserStages {
		if len(s) > 1 {
			panic("Pipeline stages must contain only one key:")
		}

		switch s {
		case "regex":
			var cfg2 parsers.Config
			err := mapstructure.Decode(rg, &cfg2)
			if err != nil {
				panic(err)
			}
		}
	}
	return Processor{}, nil
}

func (p *Processor) Process(labels *model.LabelSet, time time.Time, entry string) (time.Time, string, error) {
	t := time
	e := entry
	var err error
	//debug log labels, time, and string
	for _, parser := range p.parsers {
		t, e, err = parser.Parse(labels, t, e)
		if err != nil {
			//Log error
			//FIXME how do we proceed? panic??
			//if output is defined stages should
		}
		//debug log labels, time, and string
	}
	return t, e, nil
}
