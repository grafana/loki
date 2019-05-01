package stages

import (
	"github.com/go-kit/kit/log"
)

// NewDocker creates a Docker json log format specific pipeline stage.
func NewDocker(logger log.Logger) (Stage, error) {
	config := map[string]interface{}{
		"timestamp": map[string]interface{}{
			"source": "time",
			"format": "RFC3339",
		},
		"labels": map[string]interface{}{
			"stream": map[string]interface{}{
				"source": "stream",
			},
		},
		"output": map[string]interface{}{
			"source": "log",
		},
	}
	return NewJSON(logger, config)
}

// NewCri creates a CRI format specific pipeline stage
func NewCri(logger log.Logger) (Stage, error) {
	config := map[string]interface{}{
		"expression": "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$",
		"timestamp": map[string]interface{}{
			"source": "time",
			"format": "RFC3339Nano",
		},
		"labels": map[string]interface{}{
			"stream": map[string]interface{}{
				"source": "stream",
			},
		},
		"output": map[string]interface{}{
			"source": "content",
		},
	}
	return NewRegex(logger, config)
}
