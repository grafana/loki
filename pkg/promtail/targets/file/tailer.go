package file

import (
	"os"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/hpcloud/tail"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/util"
)

type tailer struct {
	logger    log.Logger
	handler   api.EntryHandler
	positions positions.Positions

	path string
	tail *tail.Tail

	posAndSizeMtx sync.Mutex

	running *atomic.Bool
	quit    chan struct{}
	done    chan struct{}
}

func newTailer(logger log.Logger, handler api.EntryHandler, positions positions.Positions, path string) (*tailer, error) {
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
		Follow:    true,
		Poll:      true,
		ReOpen:    true,
		MustExist: true,
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
		path:      path,
		tail:      tail,
		running:   atomic.NewBool(false),
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	tail.Logger = util.NewLogAdapter(logger)

	go tailer.run()
	filesActive.Add(1.)
	return tailer, nil
}

func (t *tailer) run() {
	level.Info(t.logger).Log("msg", "start tailing file", "path", t.path)
	positionSyncPeriod := t.positions.SyncPeriod()
	positionWait := time.NewTicker(positionSyncPeriod)
	t.running.Store(true)

	// This function runs in a goroutine, if it exits this tailer will never do any more tailing.
	// Clean everything up.
	defer func() {
		err := t.tail.Stop()
		if err != nil {
			level.Error(t.logger).Log("msg", "error stopping tailer when exiting tail goroutine", "path", t.path, "error", err)
		}

		positionWait.Stop()
		t.cleanupMetrics()
		t.running.Store(false)

		close(t.done)
	}()

	for {
		select {
		case <-positionWait.C:
			err := t.markPositionAndSize()
			if err != nil {
				level.Error(t.logger).Log("msg", "error getting tail position and/or size, stopping tailer", "path", t.path, "error", err)
				return
			}

		case line, ok := <-t.tail.Lines:
			if !ok {
				return
			}

			// Note currently the tail implementation hardcodes Err to nil, this should never hit.
			if line.Err != nil {
				level.Error(t.logger).Log("msg", "error reading line", "path", t.path, "error", line.Err)
				continue
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

func (t *tailer) markPositionAndSize() error {
	// Lock this update as there are 2 timers calling this routine, the sync in filetarget and the positions sync in this file.
	t.posAndSizeMtx.Lock()
	defer t.posAndSizeMtx.Unlock()

	pos, err := t.tail.Tell()
	if err != nil {
		return err
	}
	readBytes.WithLabelValues(t.path).Set(float64(pos))
	t.positions.Put(t.path, pos)

	size, err := t.tail.Size()
	if err != nil {
		return err
	}
	totalBytes.WithLabelValues(t.path).Set(float64(size))

	return nil
}

func (t *tailer) stop() {
	// Save the current position before shutting down tailer
	err := t.markPositionAndSize()
	if err != nil {
		level.Error(t.logger).Log("msg", "error marking file position when stopping tailer", "path", t.path, "error", err)
	}
	close(t.quit)
	<-t.done
	level.Info(t.logger).Log("msg", "stopped tailing file", "path", t.path)
	return
}

func (t *tailer) isRunning() bool {
	return t.running.Load()
}

// cleanupMetrics removes all metrics exported by this tailer
func (t *tailer) cleanupMetrics() {
	// When we stop tailing the file, also un-export metrics related to the file
	filesActive.Add(-1.)
	readLines.DeleteLabelValues(t.path)
	readBytes.DeleteLabelValues(t.path)
	totalBytes.DeleteLabelValues(t.path)
	logLengthHistogram.DeleteLabelValues(t.path)
}
