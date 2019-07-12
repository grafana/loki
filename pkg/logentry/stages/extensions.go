package stages

import (
	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
)

const RFC3339Nano = "RFC3339Nano"

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
				"timestamp",
				RFC3339Nano,
			}},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				"output",
			},
		}}
	return NewPipeline(logger, stages, nil, registerer)
}

// NewCRI creates a CRI format specific pipeline stage
func NewCRI(logger log.Logger, registerer prometheus.Registerer) (Stage, error) {
	stages := PipelineStages{
		PipelineStage{
			StageTypeRegex: RegexConfig{
				"^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$",
			},
		},
		PipelineStage{
			StageTypeLabel: LabelsConfig{
				"stream": nil,
			},
		},
		PipelineStage{
			StageTypeTimestamp: TimestampConfig{
				"time",
				RFC3339Nano,
			},
		},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				"content",
			},
		},
	}
	return NewPipeline(logger, stages, nil, registerer)
}
