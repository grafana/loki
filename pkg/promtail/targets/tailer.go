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
)

type tailer struct {
	logger    log.Logger
	handler   api.EntryHandler
	positions *positions.Positions

	path     string
	filename string
	tail     *tail.Tail

	quit chan struct{}
	done chan struct{}
}

func newTailer(logger log.Logger, handler api.EntryHandler, positions *positions.Positions, path string) (*tailer, error) {
	filename := path
	var reOpen bool

	// Check if the path requested is a symbolic link
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		filename, err = os.Readlink(path)
		if err != nil {
			return nil, err
		}

		// if we are tailing a symbolic link then we need to automatically re-open
		// as we wont get a Create event when a file is rotated.
		reOpen = true
	}

	tail, err := tail.TailFile(filename, tail.Config{
		Follow: true,
		ReOpen: reOpen,
		Location: &tail.SeekInfo{
			Offset: positions.Get(filename),
			Whence: 0,
		},
	})
	if err != nil {
		return nil, err
	}

	tailer := &tailer{
		logger:    logger,
		handler:   api.AddLabelsMiddleware(model.LabelSet{filenameLabel: model.LabelValue(path)}).Wrap(handler),
		positions: positions,

		path:     path,
		filename: filename,
		tail:     tail,
		quit:     make(chan struct{}),
		done:     make(chan struct{}),
	}
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
			readBytes.WithLabelValues(t.path).Add(float64(len(line.Text)))
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
	level.Debug(t.logger).Log("path", t.path, "filename", t.filename, "current_position", pos)
	t.positions.Put(t.filename, pos)
	return nil
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
	level.Info(t.logger).Log("msg", "stopped tailing file", "path", t.path)
	return err
}

func (t *tailer) cleanup() {
	t.positions.Remove(t.filename)
}
