package stages

import (
	"strings"

	"github.com/go-kit/kit/log"
)

type dockerRecombineStage struct {
	logger log.Logger
}

func newDockerRecombine(logger log.Logger) (Stage, error) {
	return &dockerRecombineStage{
		logger: log.With(logger,
			"component", "stage",
			"type", StageTypeDockerRecombine,
		),
	}, nil
}

func (*dockerRecombineStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		logBufs := make(map[string]*strings.Builder)
		for e := range in {
			stream, ok := e.Extracted["stream"].(string)
			if !ok {
				out <- e
				continue
			}
			buf, ok := logBufs[stream]
			if !ok {
				buf = new(strings.Builder)
				logBufs[stream] = buf
			}
			// FIXME: OK to "drop" log with valid stream and non-string msg?
			logStr, _ := e.Extracted["output"].(string)
			buf.WriteString(logStr)
			eol := len(logStr) > 0 && logStr[len(logStr)-1] == '\n'
			if eol {
				e.Extracted["output"] = buf.String()
				buf.Reset()
				out <- e
			}
		}
	}()
	return out
}

func (*dockerRecombineStage) Name() string {
	return StageTypeDockerRecombine
}
