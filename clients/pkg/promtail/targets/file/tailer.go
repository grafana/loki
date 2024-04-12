package file

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/tail"
	"github.com/grafana/tail/watch"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

type tailer struct {
	metrics   *Metrics
	logger    log.Logger
	handler   api.EntryHandler
	positions positions.Positions

	path string
	tail *tail.Tail

	posAndSizeMtx sync.Mutex
	stopOnce      sync.Once

	running *atomic.Bool
	posquit chan struct{}
	posdone chan struct{}
	done    chan struct{}

	decoder *encoding.Decoder
}

func newTailer(metrics *Metrics, logger log.Logger, handler api.EntryHandler, positions positions.Positions, pollOptions watch.PollingFileWatcherOptions, path string, encoding string) (*tailer, error) {
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
		Logger:      util.NewLogAdapter(logger),
		PollOptions: pollOptions,
	})
	if err != nil {
		return nil, err
	}

	logger = log.With(logger, "component", "tailer")
	tailer := &tailer{
		metrics:   metrics,
		logger:    logger,
		handler:   api.AddLabelsMiddleware(model.LabelSet{FilenameLabel: model.LabelValue(path)}).Wrap(handler),
		positions: positions,
		path:      path,
		tail:      tail,
		running:   atomic.NewBool(false),
		posquit:   make(chan struct{}),
		posdone:   make(chan struct{}),
		done:      make(chan struct{}),
	}

	if encoding != "" {
		level.Info(tailer.logger).Log("msg", "Will decode messages", "from", encoding, "to", "UTF8")
		encoder, err := ianaindex.IANA.Encoding(encoding)
		if err != nil {
			return nil, errors.Wrap(err, "error doing IANA encoding")
		}
		decoder := encoder.NewDecoder()
		tailer.decoder = decoder
	}

	go tailer.readLines()
	go tailer.updatePosition()
	metrics.filesActive.Add(1.)
	return tailer, nil
}

// updatePosition is run in a goroutine and checks the current size of the file and saves it to the positions file
// at a regular interval. If there is ever an error it stops the tailer and exits, the tailer will be re-opened
// by the filetarget sync method if it still exists and will start reading from the last successful entry in the
// positions file.
func (t *tailer) updatePosition() {
	positionSyncPeriod := t.positions.SyncPeriod()
	positionWait := time.NewTicker(positionSyncPeriod)
	defer func() {
		positionWait.Stop()
		level.Info(t.logger).Log("msg", "position timer: exited", "path", t.path)
		// NOTE: metrics must be cleaned up after the position timer exits, as MarkPositionAndSize() updates metrics.
		t.cleanupMetrics()
		close(t.posdone)
	}()

	for {
		select {
		case <-positionWait.C:
			err := t.MarkPositionAndSize()
			if err != nil {
				level.Error(t.logger).Log("msg", "position timer: error getting tail position and/or size, stopping tailer", "path", t.path, "error", err)
				err := t.tail.Stop()
				if err != nil {
					level.Error(t.logger).Log("msg", "position timer: error stopping tailer", "path", t.path, "error", err)
				}
				return
			}
		case <-t.posquit:
			return
		}
	}
}

// readLines runs in a goroutine and consumes the t.tail.Lines channel from the underlying tailer.
// it will only exit when that channel is closed. This is important to avoid a deadlock in the underlying
// tailer which can happen if there are unread lines in this channel and the Stop method on the tailer
// is called, the underlying tailer will never exit if there are unread lines in the t.tail.Lines channel
func (t *tailer) readLines() {
	level.Info(t.logger).Log("msg", "tail routine: started", "path", t.path)

	t.running.Store(true)

	// This function runs in a goroutine, if it exits this tailer will never do any more tailing.
	// Clean everything up.
	defer func() {
		t.running.Store(false)
		level.Info(t.logger).Log("msg", "tail routine: exited", "path", t.path)
		close(t.done)
		// Shut down the position marker thread
		close(t.posquit)
	}()
	entries := t.handler.Chan()
	for {
		line, ok := <-t.tail.Lines
		if !ok {
			level.Info(t.logger).Log("msg", "tail routine: tail channel closed, stopping tailer", "path", t.path, "reason", t.tail.Tomb.Err())
			return
		}

		// Note currently the tail implementation hardcodes Err to nil, this should never hit.
		if line.Err != nil {
			level.Error(t.logger).Log("msg", "tail routine: error reading line", "path", t.path, "error", line.Err)
			continue
		}

		var text string
		if t.decoder != nil {
			var err error
			text, err = t.convertToUTF8(line.Text)
			if err != nil {
				level.Debug(t.logger).Log("msg", "failed to convert encoding", "error", err)
				t.metrics.encodingFailures.WithLabelValues(t.path).Inc()
				text = fmt.Sprintf("the requested encoding conversion for this line failed in Promtail/Grafana Agent: %s", err.Error())
			}
		} else {
			text = line.Text
		}

		t.metrics.readLines.WithLabelValues(t.path).Inc()
		entries <- api.Entry{
			Labels: model.LabelSet{},
			Entry: logproto.Entry{
				Timestamp: line.Time,
				Line:      text,
			},
		}
	}
}

func (t *tailer) MarkPositionAndSize() error {
	// Lock this update as there are 2 timers calling this routine, the sync in filetarget and the positions sync in this file.
	t.posAndSizeMtx.Lock()
	defer t.posAndSizeMtx.Unlock()

	size, err := t.tail.Size()
	if err != nil {
		// If the file no longer exists, no need to save position information
		if err == os.ErrNotExist {
			level.Info(t.logger).Log("msg", "skipping update of position for a file which does not currently exist", "path", t.path)
			return nil
		}
		return err
	}

	pos, err := t.tail.Tell()
	if err != nil {
		return err
	}

	// Update metrics and positions file all together to avoid race conditions when `t.tail` is stopped.
	t.metrics.totalBytes.WithLabelValues(t.path).Set(float64(size))
	t.metrics.readBytes.WithLabelValues(t.path).Set(float64(pos))
	t.positions.Put(t.path, pos)

	return nil
}

func (t *tailer) Stop() {
	// stop can be called by two separate threads in filetarget, to avoid a panic closing channels more than once
	// we wrap the stop in a sync.Once.
	t.stopOnce.Do(func() {
		// Save the current position before shutting down tailer
		err := t.MarkPositionAndSize()
		if err != nil {
			level.Error(t.logger).Log("msg", "error marking file position when stopping tailer", "path", t.path, "error", err)
		}

		// Stop the underlying tailer
		err = t.tail.Stop()
		if err != nil {
			level.Error(t.logger).Log("msg", "error stopping tailer", "path", t.path, "error", err)
		}
		// Wait for readLines() to consume all the remaining messages and exit when the channel is closed
		<-t.done
		// Wait for the position marker thread to exit
		<-t.posdone
		level.Info(t.logger).Log("msg", "stopped tailing file", "path", t.path)
		t.handler.Stop()
	})
}

func (t *tailer) IsRunning() bool {
	return t.running.Load()
}

func (t *tailer) convertToUTF8(text string) (string, error) {
	res, _, err := transform.String(t.decoder, text)
	if err != nil {
		return "", errors.Wrap(err, "error decoding text")
	}

	return res, nil
}

// cleanupMetrics removes all metrics exported by this tailer
func (t *tailer) cleanupMetrics() {
	// When we stop tailing the file, also un-export metrics related to the file
	t.metrics.filesActive.Add(-1.)
	t.metrics.readLines.DeleteLabelValues(t.path)
	t.metrics.readBytes.DeleteLabelValues(t.path)
	t.metrics.totalBytes.DeleteLabelValues(t.path)
}

func (t *tailer) Path() string {
	return t.path
}
