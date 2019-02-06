package targets

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
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
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
	filenameLabel = "__filename__"
)

// FileTarget describes a particular set of logs.
type FileTarget struct {
	logger log.Logger

	handler   api.EntryHandler
	positions *positions.Positions

	watcher *fsnotify.Watcher
	watches map[string]struct{}
	path    string
	quit    chan struct{}
	done    chan struct{}

	tails map[string]*tailer

	targetConfig *api.TargetConfig
}

// NewFileTarget create a new FileTarget.
func NewFileTarget(logger log.Logger, handler api.EntryHandler, positions *positions.Positions, path string, labels model.LabelSet, targetConfig *api.TargetConfig) (*FileTarget, error) {

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "fsnotify.NewWatcher")
	}

	t := &FileTarget{
		logger:       logger,
		watcher:      watcher,
		path:         path,
		handler:      api.AddLabelsMiddleware(labels).Wrap(handler),
		positions:    positions,
		quit:         make(chan struct{}),
		done:         make(chan struct{}),
		tails:        map[string]*tailer{},
		targetConfig: targetConfig,
	}

	err = t.sync()
	if err != nil {
		return nil, errors.Wrap(err, "target.sync")
	}

	go t.run()
	return t, nil
}

// Stop the target.
func (t *FileTarget) Stop() {
	close(t.quit)
	<-t.done
}

func (t *FileTarget) run() {
	defer func() {
		helpers.LogError("closing watcher", t.watcher.Close)
		for _, v := range t.tails {
			helpers.LogError("updating tailer last position", v.markPosition)
			helpers.LogError("stopping tailer", v.stop)
		}
		level.Debug(t.logger).Log("msg", "watcher closed, tailer stopped, positions saved")
		close(t.done)
	}()

	ticker := time.NewTicker(t.targetConfig.SyncPeriod)

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
		case <-ticker.C:
			err := t.sync()
			if err != nil {
				level.Error(t.logger).Log("msg", "error running sync function", "error", err)
			}
		case <-t.quit:
			return
		}
	}
}

func (t *FileTarget) sync() error {

	// Find list of directories to add to watcher.
	var err error
	path, err := filepath.Abs(t.path)
	if err != nil {
		return errors.Wrap(err, "filepath.Abs")
	}

	// Gets current list of files to tail.
	matches, err := filepath.Glob(path)
	if err != nil {
		return errors.Wrap(err, "filepath.Glob")
	}

	// Get the current unique set of dirs to watch.
	dirs := make(map[string]struct{})
	for _, p := range matches {
		dirs[filepath.Dir(p)] = struct{}{}
	}

	// If no files exist yet watch the directory specified in the path.
	if matches == nil {
		dirs[filepath.Dir(path)] = struct{}{}
	}

	// Add any directories which are not already being watched.
	for dir := range dirs {
		if _, ok := t.watches[dir]; !ok {
			level.Debug(t.logger).Log("msg", "sync() watching new directory", "directory", dir)
			if err := t.watcher.Add(dir); err != nil {
				level.Error(t.logger).Log("msg", "sync() error adding directory to watcher", "error", err)
			}
		}
	}

	// Remove any directories which no longer need watching.
	if len(t.watches) > 0 && len(t.watches) != len(dirs) {
		for watched := range t.watches {
			if _, ok := dirs[watched]; !ok {
				// The existing directory being watched is no longer in our list of dirs to watch so we can remove it.
				level.Debug(t.logger).Log("msg", "sync() removing directory from watcher", "directory", watched)
				err = t.watcher.Remove(watched)
				if err != nil {
					level.Error(t.logger).Log("msg", "sync() failed to remove directory from watcher", "error", err)
				}

				// Shutdown and cleanup and tailers for files in directories no longer being watched.
				for tailedFile, tailer := range t.tails {
					if filepath.Dir(tailedFile) == watched {
						helpers.LogError("stopping tailer", tailer.stop)
						tailer.cleanup() //FIXME should we defer this to the cleanup function?
						delete(t.tails, tailedFile)
					}
				}

			}
		}
	}

	t.watches = dirs

	// Start tailing all of the matched files if not already doing so.
	for _, p := range matches {
		if _, ok := t.tails[p]; !ok {
			fi, err := os.Stat(p)
			if err != nil {
				level.Error(t.logger).Log("msg", "sync() failed to stat file", "error", err, "filename", p)
				continue
			}
			if fi.IsDir() {
				level.Debug(t.logger).Log("msg", "sync() skipping matched dir", "filename", p)
				continue
			}

			level.Debug(t.logger).Log("msg", "sync() tailing new file", "filename", p)
			tailer, err := newTailer(t.logger, t.handler, t.positions, p)
			if err != nil {
				level.Error(t.logger).Log("msg", "sync() failed to tail file", "error", err, "filename", p)
				continue
			}
			t.tails[p] = tailer
		}
	}
	return nil
}

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
