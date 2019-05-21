package stages

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
)

const RFC3339Nano = "RFC3339Nano"

// NewDocker creates a Docker json log format specific pipeline stage.
func NewDocker(logger log.Logger) (Stage, error) {

	// JSON Stage to extract output, stream, and timestamp
	jsonCfg := &JSONConfig{
		Expressions: map[string]string{
			"output":    "log",
			"stream":    "stream",
			"timestamp": "time",
		},
	}
	jsonStage, err := New(logger, StageTypeJSON, jsonCfg, nil)
	if err != nil {
		return nil, err
	}

	// Set the stream as a label
	lblCfg := &LabelsConfig{
		"stream": nil,
	}
	labelStage, err := New(logger, StageTypeLabel, lblCfg, nil)
	if err != nil {
		return nil, err
	}

	// Parse the time into the current timestamp
	t := "timestamp"
	f := RFC3339Nano
	tsCfg := &TimestampConfig{
		&t,
		&f,
	}
	tsStage, err := New(logger, StageTypeTimestamp, tsCfg, nil)
	if err != nil {
		return nil, err
	}

	// Set the output to the log message
	o := "output"
	outputCfg := &OutputConfig{
		&o,
	}
	outputStage, err := New(logger, StageTypeOutput, outputCfg, nil)
	if err != nil {
		return nil, err
	}

	return StageFunc(func(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
		jsonStage.Process(labels, extracted, t, entry)
		labelStage.Process(labels, extracted, t, entry)
		tsStage.Process(labels, extracted, t, entry)
		outputStage.Process(labels, extracted, t, entry)
	}), nil
}

// NewCRI creates a CRI format specific pipeline stage
func NewCRI(logger log.Logger) (Stage, error) {
	regexCfg := &RegexConfig{
		"^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$",
	}
	regexStage, err := New(logger, StageTypeRegex, regexCfg, nil)
	if err != nil {
		return nil, err
	}

	// Set the stream as a label
	lblCfg := &LabelsConfig{
		"stream": nil,
	}
	labelStage, err := New(logger, StageTypeLabel, lblCfg, nil)
	if err != nil {
		return nil, err
	}

	// Parse the time into the current timestamp
	t := "time"
	f := RFC3339Nano
	tsCfg := &TimestampConfig{
		&t,
		&f,
	}
	tsStage, err := New(logger, StageTypeTimestamp, tsCfg, nil)
	if err != nil {
		return nil, err
	}

	// Set the output to the log message
	o := "content"
	outputCfg := &OutputConfig{
		&o,
	}
	outputStage, err := New(logger, StageTypeOutput, outputCfg, nil)
	if err != nil {
		return nil, err
	}

	return StageFunc(func(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
		regexStage.Process(labels, extracted, t, entry)
		labelStage.Process(labels, extracted, t, entry)
		tsStage.Process(labels, extracted, t, entry)
		outputStage.Process(labels, extracted, t, entry)
	}), nil
}
