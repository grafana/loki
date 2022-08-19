package scribe

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/grafana/scribe/plumbing"
	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/stringutil"
	"github.com/sirupsen/logrus"
)

func newFilesystemState(u *url.URL) (pipeline.StateHandler, error) {
	path := u.Path
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			path = filepath.Join(path, fmt.Sprintf("%s.json", stringutil.Random(8)))
		}
	}

	return pipeline.NewFilesystemState(path)
}

var states = map[string]func(*url.URL) (pipeline.StateHandler, error){
	"file": newFilesystemState,
	"fs":   newFilesystemState,
}

func GetState(val string, log logrus.FieldLogger, args *plumbing.PipelineArgs) (*pipeline.State, error) {
	u, err := url.Parse(val)
	if err != nil {
		return nil, err
	}

	fallback := []pipeline.StateReader{
		pipeline.StateReaderWithLogs(log.WithField("state", "arguments"), pipeline.NewArgMapReader(args.ArgMap)),
	}

	if args.CanStdinPrompt {
		fallback = append(fallback, pipeline.StateReaderWithLogs(log.WithField("state", "stdin"), pipeline.NewStdinReader(os.Stdin, os.Stdout)))
	}

	if v, ok := states[u.Scheme]; ok {
		handler, err := v(u)
		if err != nil {
			return nil, err
		}

		return &pipeline.State{
			Handler:  pipeline.StateHandlerWithLogs(log.WithField("state", u.Scheme), handler),
			Fallback: fallback,
			Log:      log,
		}, nil
	}

	return nil, fmt.Errorf("state URL scheme '%s' not recognized", val)
}
