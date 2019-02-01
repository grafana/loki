package promtail

import (
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/hpcloud/tail"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	fsnotify "gopkg.in/fsnotify.v1"

	"github.com/grafana/loki/pkg/helpers"
)

var (
	readBytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "read_bytes_total",
		Help:      "Number of bytes read.",
	}, []string{"path"})

	readLines = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "read_lines_total",
		Help:      "Number of lines read.",
	}, []string{"path"})

	filesActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "files_active_total",
		Help:      "Number of active files.",
	})
)

const (
	filename = "__filename__"
)

// Target describes a particular set of logs.
type Target struct {
	logger log.Logger

	handler   EntryHandler
	positions *Positions

	watcher *fsnotify.Watcher
	path    string
	quit    chan struct{}
	done    chan struct{}

	tails map[string]*tailer
}

// NewTarget create a new Target.
func NewTarget(logger log.Logger, handler EntryHandler, positions *Positions, path string, labels model.LabelSet) (*Target, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Abs")
	}
	matches, err := filepath.Glob(path)
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Glob")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "fsnotify.NewWatcher")
	}

	// get the current unique set of dirs to watch.
	dirs := make(map[string]struct{})
	for _, p := range matches {
		dirs[filepath.Dir(p)] = struct{}{}
	}

	// If no files exist yet watch the directory specified in the path.
	if matches == nil {
		dirs[filepath.Dir(path)] = struct{}{}
	}

	// watch each dir for any new files.
	for dir := range dirs {
		level.Debug(logger).Log("msg", "watching new directory", "directory", dir)
		if err := watcher.Add(dir); err != nil {
			helpers.LogError("closing watcher", watcher.Close)
			return nil, errors.Wrap(err, "watcher.Add")
		}
	}

	t := &Target{
		logger:    logger,
		watcher:   watcher,
		path:      path,
		handler:   addLabelsMiddleware(labels).Wrap(handler),
		positions: positions,
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
		tails:     map[string]*tailer{},
	}

	// start tailing all of the matched files
	for _, p := range matches {
		fi, err := os.Stat(p)
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to stat file", "error", err, "filename", p)
			continue
		}
		if fi.IsDir() {
			level.Debug(t.logger).Log("msg", "skipping matched dir", "filename", p)
			continue
		}

		tailer, err := newTailer(t.logger, t.handler, t.positions, p)
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to tail file", "error", err, "filename", p)
			continue
		}
		t.tails[p] = tailer
	}

	go t.run()
	return t, nil
}

// Stop the target.
func (t *Target) Stop() {
	close(t.quit)
	<-t.done
}

func (t *Target) run() {
	defer func() {
		helpers.LogError("closing watcher", t.watcher.Close)
		for _, v := range t.tails {
			helpers.LogError("updating tailer last position", v.markPosition)
			helpers.LogError("stopping tailer", v.stop)
		}
		level.Debug(t.logger).Log("msg", "watcher closed, tailer stopped, positions saved")
		close(t.done)
	}()

	for {
		select {
		case event := <-t.watcher.Events:
			switch event.Op {
			case fsnotify.Create:
				if tailer, ok := t.tails[event.Name]; ok {
					level.Info(t.logger).Log("msg", "create for file being tailed. Will close and re-open", "filename", event.Name)
					helpers.LogError("stopping tailer", tailer.stop)
					delete(t.tails, event.Name)
				}
				matched, err := filepath.Match(t.path, event.Name)
				if err != nil {
					level.Error(t.logger).Log("msg", "failed to match file", "error", err, "filename", event.Name)
					continue
				}
				if !matched {
					level.Debug(t.logger).Log("msg", "new file does not match glob", "filename", event.Name)
					continue
				}
				tailer, err := newTailer(t.logger, t.handler, t.positions, event.Name)
				if err != nil {
					level.Error(t.logger).Log("msg", "failed to tail file", "error", err, "filename", event.Name)
					continue
				}

				level.Debug(t.logger).Log("msg", "tailing new file", "filename", event.Name)
				t.tails[event.Name] = tailer

			case fsnotify.Remove:
				tailer, ok := t.tails[event.Name]
				if ok {
					helpers.LogError("stopping tailer", tailer.stop)
					tailer.cleanup()
					delete(t.tails, event.Name)
				}
			case fsnotify.Rename:
				// Rename is only issued on the original file path; the new name receives a Create event
				tailer, ok := t.tails[event.Name]
				if ok {
					helpers.LogError("stopping tailer", tailer.stop)
					tailer.cleanup()
					delete(t.tails, event.Name)
				}

			default:
				level.Debug(t.logger).Log("msg", "got unknown event", "event", event)
			}
		case err := <-t.watcher.Errors:
			level.Error(t.logger).Log("msg", "error from fswatch", "error", err)
		case <-t.quit:
			return
		}
	}
}

type tailer struct {
	logger    log.Logger
	handler   EntryHandler
	positions *Positions

	path     string
	filename string
	tail     *tail.Tail

	quit chan struct{}
	done chan struct{}
}

func newTailer(logger log.Logger, handler EntryHandler, positions *Positions, path string) (*tailer, error) {
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
		handler:   addLabelsMiddleware(model.LabelSet{"filename": model.LabelValue(path)}).Wrap(handler),
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
	positionSyncPeriod := t.positions.cfg.SyncPeriod
	positionWait := time.NewTicker(positionSyncPeriod)

	defer func() {
		level.Info(t.logger).Log("msg", "stopped tailing file", "path", t.path)
		positionWait.Stop()
		err := t.markPosition()
		if err != nil {
			level.Error(t.logger).Log("msg", "error getting tail position", "path", t.path, "error", err)
		}
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
	close(t.quit)
	<-t.done
	filesActive.Add(-1.)
	return t.tail.Stop()
}

func (t *tailer) cleanup() {
	t.positions.Remove(t.filename)
}
