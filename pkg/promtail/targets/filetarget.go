package targets

import (
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/bmatcuk/doublestar"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

// Config describes behavior for Target
type Config struct {
	SyncPeriod time.Duration `yaml:"sync_period"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(flags *flag.FlagSet) {
	flags.DurationVar(&cfg.SyncPeriod, "target.sync-period", 10*time.Second, "Period to resync directories being watched and files being tailed.")
}

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

	targetConfig *Config
}

// NewFileTarget create a new FileTarget.
func NewFileTarget(logger log.Logger, handler api.EntryHandler, positions *positions.Positions, path string, labels model.LabelSet, targetConfig *Config) (*FileTarget, error) {

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "filetarget.fsnotify.NewWatcher")
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
		return nil, errors.Wrap(err, "filetarget.sync")
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
				// If the file was a symlink we don't get a Remove notification if the symlink resolves to a non watched directory.
				// Close and re-open the tailer to make sure we tail the new file.
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
				t.startTailing([]string{event.Name})
			case fsnotify.Remove:
				t.stopTailing([]string{event.Name})
			case fsnotify.Rename:
				// Rename is only issued on the original file path; the new name receives a Create event
				t.stopTailing([]string{event.Name})
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
	path, err := filepath.Abs(t.path)
	if err != nil {
		return errors.Wrap(err, "filetarget.sync.filepath.Abs")
	}

	// Gets current list of files to tail.
	matches, err := doublestar.Glob(path)
	if err != nil {
		return errors.Wrap(err, "filetarget.sync.filepath.Glob")
	}

	// Get the current unique set of dirs to watch.
	dirs := map[string]struct{}{}
	for _, p := range matches {
		dirs[filepath.Dir(p)] = struct{}{}
	}

	// Add any directories which are not already being watched.
	toStartWatching := missing(t.watches, dirs)
	t.startWatching(toStartWatching)

	// Remove any directories which no longer need watching.
	toStopWatching := missing(dirs, t.watches)
	t.stopWatching(toStopWatching)

	// fsnotify.Watcher doesn't allow us to see what is currently being watched so we have to track it ourselves.
	t.watches = dirs

	// Start tailing all of the matched files if not already doing so.
	t.startTailing(matches)

	// Stop tailing any files which no longer exist
	toStopTailing := toStopTailing(matches, t.tails)
	t.stopTailing(toStopTailing)

	return nil
}

func (t *FileTarget) startWatching(dirs map[string]struct{}) {
	for dir := range dirs {
		if _, ok := t.watches[dir]; ok {
			continue
		}
		level.Debug(t.logger).Log("msg", "watching new directory", "directory", dir)
		if err := t.watcher.Add(dir); err != nil {
			level.Error(t.logger).Log("msg", "error adding directory to watcher", "error", err)
		}
	}
}

func (t *FileTarget) stopWatching(dirs map[string]struct{}) {
	for dir := range dirs {
		if _, ok := t.watches[dir]; !ok {
			continue
		}
		level.Debug(t.logger).Log("msg", "removing directory from watcher", "directory", dir)
		err := t.watcher.Remove(dir)
		if err != nil {
			level.Error(t.logger).Log("msg", " failed to remove directory from watcher", "error", err)
		}
	}
}

func (t *FileTarget) startTailing(ps []string) {
	for _, p := range ps {
		if _, ok := t.tails[p]; ok {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to tail file, stat failed", "error", err, "filename", p)
		}
		if fi.IsDir() {
			level.Error(t.logger).Log("msg", "failed to tail file", "error", "file is a directory", "filename", p)
		}
		level.Debug(t.logger).Log("msg", "tailing new file", "filename", p)
		tailer, err := newTailer(t.logger, t.handler, t.positions, p)
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to start tailer", "error", err, "filename", p)
		}
		t.tails[p] = tailer
	}
}

func (t *FileTarget) stopTailing(ps []string) {
	for _, p := range ps {
		if tailer, ok := t.tails[p]; ok {
			helpers.LogError("stopping tailer", tailer.stop)
			tailer.cleanup()
			delete(t.tails, p)
		}
	}
}

func toStopTailing(nt []string, et map[string]*tailer) []string {
	// Make a set of all existing tails
	existingTails := make(map[string]struct{}, len(et))
	for file := range et {
		existingTails[file] = struct{}{}
	}
	// Make a set of what we are about to start tailing
	newTails := make(map[string]struct{}, len(nt))
	for _, p := range nt {
		newTails[p] = struct{}{}
	}
	// Find the tails in our existing which are not in the new, these need to be stopped!
	ts := missing(newTails, existingTails)
	ta := make([]string, len(ts))
	i := 0
	for t := range ts {
		ta[i] = t
		i++
	}
	return ta
}

// Returns the elements from set b which are missing from set a
func missing(as map[string]struct{}, bs map[string]struct{}) map[string]struct{} {
	c := map[string]struct{}{}
	for a := range bs {
		if _, ok := as[a]; !ok {
			c[a] = struct{}{}
		}
	}
	return c
}
