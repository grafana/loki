package stages

import (
	"github.com/go-kit/kit/log"
)

const RFC3339Nano = "RFC3339Nano"

// NewDocker creates a Docker json log format specific pipeline stage.
func NewDocker(logger log.Logger, jobName string) (Stage, error) {
	t := "timestamp"
	f := RFC3339Nano
	o := "output"
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
				&t,
				&f,
			}},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				&o,
			},
		}}

	return NewPipeline(logger, stages, jobName+"_docker")
}

// NewCRI creates a CRI format specific pipeline stage
func NewCRI(logger log.Logger, jobName string) (Stage, error) {
	t := "time"
	f := RFC3339Nano
	o := "content"
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
				&t,
				&f,
			},
		},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				&o,
			},
		},
	}
	return NewPipeline(logger, stages, jobName+"_cri")
}
