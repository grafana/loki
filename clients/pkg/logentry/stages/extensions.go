package stages

import (
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	RFC3339Nano         = "RFC3339Nano"
	MaxPartialLinesSize = 100 // Max buffer size to hold partial lines.
)

// NewDocker creates a Docker json log format specific pipeline stage.
func NewDocker(logger log.Logger, registerer prometheus.Registerer) (Stage, error) {
	stages := PipelineStages{
		PipelineStage{
			StageTypeJSON: JSONConfig{
				Expressions: map[string]string{
					"output":    "log",
					"stream":    "stream",
					"timestamp": "time",
				},
			}},
		PipelineStage{
			StageTypeLabel: LabelsConfig{
				"stream": nil,
			}},
		PipelineStage{
			StageTypeTimestamp: TimestampConfig{
				Source: "timestamp",
				Format: RFC3339Nano,
			}},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				"output",
			},
		}}
	return NewPipeline(logger, stages, nil, registerer)
}

type cri struct {
	// bounded buffer for CRI-O Partial logs lines (identified with tag `P` till we reach first `F`)
	partialLines []string
	base         *Pipeline
}

// implement Stage interface
func (c *cri) Name() string {
	return "cri"
}

// implements Stage interface
func (c *cri) Run(entry chan Entry) chan Entry {
	entry = c.base.Run(entry)

	in := RunWithSkip(entry, func(e Entry) (Entry, bool) {
		fmt.Println(e.Extracted)
		if e.Extracted["flags"] == "P" {
			if len(c.partialLines) >= MaxPartialLinesSize {
				c.partialLines = c.partialLines[:0]
			}
			c.partialLines = append(c.partialLines, e.Line)
			return Entry{}, true
		}
		if len(c.partialLines) > 0 {
			cpy := e.copy()
			c.partialLines = append(c.partialLines, e.Line)
			cpy.Line = strings.Join(c.partialLines, "\n")
			e = *cpy
			c.partialLines = c.partialLines[:0]
		}
		return e, false
	})

	return in
}

// NewCRI creates a CRI format specific pipeline stage
func NewCRI(logger log.Logger, registerer prometheus.Registerer) (Stage, error) {
	base := PipelineStages{
		PipelineStage{
			StageTypeRegex: RegexConfig{
				Expression: "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$",
			},
		},
		PipelineStage{
			StageTypeLabel: LabelsConfig{
				"stream": nil,
			},
		},
		PipelineStage{
			StageTypeTimestamp: TimestampConfig{
				Source: "time",
				Format: RFC3339Nano,
			},
		},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				"content",
			},
		},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				"tags",
			},
		},
	}

	p, err := NewPipeline(logger, base, nil, registerer)
	if err != nil {
		return nil, err
	}

	c := cri{
		partialLines: make([]string, 0, 100),
		base:         p,
	}
	return &c, nil
}
