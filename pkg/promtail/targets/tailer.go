package targets

import (
	"os"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/hpcloud/tail"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/util"
)

type tailer struct {
	logger    log.Logger
	handler   api.EntryHandler
	positions *positions.Positions

	path string
	tail *tail.Tail

	quit chan struct{}
	done chan struct{}
}

func newTailer(logger log.Logger, handler api.EntryHandler, positions *positions.Positions, path string) (*tailer, error) {
	// Simple check to make sure the file we are tailing doesn't
	// have a position already saved which is past the end of the file.
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	pos, err := positions.Get(path)
	if err != nil {
		return nil, err
	}

	if fi.Size() < pos {
		positions.Remove(path)
	}

	tail, err := tail.TailFile(path, tail.Config{
		Follow: true,
		Poll:   true,
		ReOpen: true,
		Location: &tail.SeekInfo{
			Offset: pos,
			Whence: 0,
		},
	})
	if err != nil {
		return nil, err
	}

	logger = log.With(logger, "component", "tailer")
	tailer := &tailer{
		logger:    logger,
		handler:   api.AddLabelsMiddleware(model.LabelSet{FilenameLabel: model.LabelValue(path)}).Wrap(handler),
		positions: positions,

		path: path,
		tail: tail,
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	tail.Logger = util.NewLogAdapater(logger)

	go tailer.run()
	filesActive.Add(1.)
	return tailer, nil
}

func (t *tailer) run() {
	level.Info(t.logger).Log("msg", "start tailing file", "path", t.path)
	positionSyncPeriod := t.positions.SyncPeriod()
	positionWait := time.NewTicker(positionSyncPeriod)

	defer func() {
		positionWait.Stop()
		close(t.done)
	}()

	for {
		select {
		case <-positionWait.C:
			err := t.markPosition()
			if err != nil {
				level.Error(t.logger).Log("msg", "error getting tail position", "path", t.path, "error", err)
				continue
			}

		case line, ok := <-t.tail.Lines:
			if !ok {
				return
			}

			if line.Err != nil {
				level.Error(t.logger).Log("msg", "error reading line", "path", t.path, "error", line.Err)
			}

			readLines.WithLabelValues(t.path).Inc()
			logLengthHistogram.WithLabelValues(t.path).Observe(float64(len(line.Text)))
			if err := t.handler.Handle(model.LabelSet{}, line.Time, line.Text); err != nil {
				level.Error(t.logger).Log("msg", "error handling line", "path", t.path, "error", err)
			}
		case <-t.quit:
			return
		}
	}
}

func (t *tailer) markPosition() error {
	pos, err := t.tail.Tell()
	if err != nil {
		return err
	}

	readBytes.WithLabelValues(t.path).Set(float64(pos))
	t.positions.Put(t.path, pos)
	return nil
}

func (t *tailer) size() (int64, error) {
	s, err := t.tail.Size()
	if err != nil {
		return 0, err
	}
	return s, nil
}

func (t *tailer) stop() error {
	// Save the current position before shutting down tailer
	err := t.markPosition()
	if err != nil {
		level.Error(t.logger).Log("msg", "error getting tail position", "path", t.path, "error", err)
	}
	err = t.tail.Stop()
	close(t.quit)
	<-t.done
	filesActive.Add(-1.)
	// When we stop tailing the file, also un-export metrics related to the file
	readBytes.DeleteLabelValues(t.path)
	totalBytes.DeleteLabelValues(t.path)
	logLengthHistogram.DeleteLabelValues(t.path)
	level.Info(t.logger).Log("msg", "stopped tailing file", "path", t.path)
	return err
}

func (t *tailer) cleanup() {
	t.positions.Remove(t.path)
}
