package plog

import (
	"strings"

	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/sirupsen/logrus"
)

func LogSteps(logger logrus.FieldLogger, steps []pipeline.Step) {
	s := make([]string, len(steps))
	for i, v := range steps {
		s[i] = v.Name
	}
	logger.Infof("[%d] step(s) %s", len(steps), strings.Join(s, " | "))
}

func LogPipelines(logger logrus.FieldLogger, pipelines []pipeline.Pipeline) {
	s := make([]string, len(pipelines))
	for i, v := range pipelines {
		s[i] = v.Name
	}
	logger.Infof("[%d] pipelines(s) %s", len(pipelines), strings.Join(s, " | "))
}
