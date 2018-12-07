package promtail

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/google/mtail/logline"
	"github.com/google/mtail/tailer"
	"github.com/google/mtail/watcher"
	"github.com/grafana/loki/pkg/helpers"
	"github.com/spf13/afero"
)

var (
	readBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "read_bytes_total",
		Help:      "Number of bytes read.",
	}, []string{"path"})
)

const (
	filename = "__filename__"
)

func init() {
	prometheus.MustRegister(readBytes)
}

// Target describes a particular set of logs.
type Target struct {
	logger  log.Logger
	tailer  *tailer.Tailer
	handler EntryHandler
	lines   chan *logline.LogLine
	path    string
	quit    chan struct{}
}

// NewTarget create a new Target.
func NewTarget(logger log.Logger, handler EntryHandler, positions *Positions, path string, labels model.LabelSet) (*Target, error) {
	watcher, err := watcher.NewLogWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "fsnotify.NewWatcher")
	}

	linesCh := make(chan *logline.LogLine)
	tailer, err := tailer.New(linesCh, afero.NewOsFs(), watcher)
	if err != nil {
		return nil, errors.Wrap(err, "tailer.New")
	}
	err = tailer.TailPattern(path)
	if err != nil {
		return nil, errors.Wrap(err, "tailer.TailPattern")
	}

	t := &Target{
		logger:  logger,
		tailer:  tailer,
		path:    path,
		handler: handler,
		quit:    make(chan struct{}),
		lines:   linesCh,
	}

	go t.run()
	return t, nil
}

// Stop the target.
func (t *Target) Stop() {
	close(t.quit)
}

func (t *Target) run() {
	defer func() {
		helpers.LogError("closing tailer", t.tailer.Close)
	}()

	for {
		select {
		case line, ok := <-t.lines:
			if !ok {
				return
			}

			readBytes.WithLabelValues(line.Filename).Add(float64(len(line.Line)))
			if err := t.handler.Handle(model.LabelSet{filename: model.LabelValue(line.Filename)}, time.Now(), line.Line); err != nil {
				level.Error(t.logger).Log("msg", "error handling line", "error", err)
			}

		case <-t.quit:
			return
		}
	}
}
