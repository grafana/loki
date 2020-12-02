package file

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
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

var (
	readBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "read_bytes_total",
		Help:      "Number of bytes read.",
	}, []string{"path"})
	totalBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "file_bytes_total",
		Help:      "Number of bytes total.",
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
	logLengthHistogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "log_entries_bytes",
		Help:      "the total count of bytes",
		Buckets:   prometheus.ExponentialBuckets(16, 2, 8),
	}, []string{"path"})
)

const (
	FilenameLabel = "filename"
)

// Config describes behavior for Target
type Config struct {
	SyncPeriod time.Duration `yaml:"sync_period"`
	Stdin      bool          `yaml:"stdin"`
}

// RegisterFlags with prefix registers flags where every name is prefixed by
// prefix. If prefix is a non-empty string, prefix should end with a period.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.SyncPeriod, prefix+"target.sync-period", 10*time.Second, "Period to resync directories being watched and files being tailed.")
	f.BoolVar(&cfg.Stdin, prefix+"stdin", false, "Set to true to pipe logs to promtail.")
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(flags *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", flags)
}

// FileTarget describes a particular set of logs.
// nolint:golint
type FileTarget struct {
	logger log.Logger

	handler          api.EntryHandler
	positions        positions.Positions
	labels           model.LabelSet
	discoveredLabels model.LabelSet

	watcher *fsnotify.Watcher
	watches map[string]struct{}
	path    string
	quit    chan struct{}
	done    chan struct{}

	tails map[string]*tailer

	targetConfig *Config
}

// NewFileTarget create a new FileTarget.
func NewFileTarget(logger log.Logger, handler api.EntryHandler, positions positions.Positions, path string, labels model.LabelSet, discoveredLabels model.LabelSet, targetConfig *Config) (*FileTarget, error) {

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "filetarget.fsnotify.NewWatcher")
	}

	t := &FileTarget{
		logger:           logger,
		watcher:          watcher,
		path:             path,
		labels:           labels,
		discoveredLabels: discoveredLabels,
		handler:          api.AddLabelsMiddleware(labels).Wrap(handler),
		positions:        positions,
		quit:             make(chan struct{}),
		done:             make(chan struct{}),
		tails:            map[string]*tailer{},
		targetConfig:     targetConfig,
	}

	err = t.sync()
	if err != nil {
		return nil, errors.Wrap(err, "filetarget.sync")
	}

	go t.run()
	return t, nil
}

// Ready if at least one file is being tailed
func (t *FileTarget) Ready() bool {
	return len(t.tails) > 0
}

// Stop the target.
func (t *FileTarget) Stop() {
	close(t.quit)
	<-t.done
	t.handler.Stop()
}

// Type implements a Target
func (t *FileTarget) Type() target.TargetType {
	return target.FileTargetType
}

// DiscoveredLabels implements a Target
func (t *FileTarget) DiscoveredLabels() model.LabelSet {
	return t.discoveredLabels
}

// Labels implements a Target
func (t *FileTarget) Labels() model.LabelSet {
	return t.labels
}

// Details implements a Target
func (t *FileTarget) Details() interface{} {
	files := map[string]int64{}
	for fileName := range t.tails {
		files[fileName], _ = t.positions.Get(fileName)
	}
	return files
}

func (t *FileTarget) run() {
	defer func() {
		helpers.LogError("closing watcher", t.watcher.Close)
		for _, v := range t.tails {
			v.stop()
		}
		level.Info(t.logger).Log("msg", "filetarget: watcher closed, tailer stopped, positions saved", "path", t.path)
		close(t.done)
	}()

	ticker := time.NewTicker(t.targetConfig.SyncPeriod)

	for {
		select {
		case event := <-t.watcher.Events:
			switch event.Op {
			case fsnotify.Create:
				matched, err := doublestar.Match(t.path, event.Name)
				if err != nil {
					level.Error(t.logger).Log("msg", "failed to match file", "error", err, "filename", event.Name)
					continue
				}
				if !matched {
					level.Debug(t.logger).Log("msg", "new file does not match glob", "filename", event.Name)
					continue
				}
				t.startTailing([]string{event.Name})
			default:
				// No-op we only care about Create events
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

	// Gets current list of files to tail.
	matches, err := doublestar.Glob(t.path)
	if err != nil {
		return errors.Wrap(err, "filetarget.sync.filepath.Glob")
	}

	if len(matches) == 0 {
		level.Debug(t.logger).Log("msg", "no files matched requested path, nothing will be tailed", "path", t.path)
	}

	// Gets absolute path for each pattern.
	for i := 0; i < len(matches); i++ {
		if !filepath.IsAbs(matches[i]) {
			path, err := filepath.Abs(matches[i])
			if err != nil {
				return errors.Wrap(err, "filetarget.sync.filepath.Abs")
			}
			matches[i] = path
		}
	}

	// Record the size of all the files matched by the Glob pattern.
	t.reportSize(matches)

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

	// Check if any running tailers have stopped because of errors and remove them from the running list
	// (They will be restarted in startTailing)
	t.pruneStoppedTailers()

	// Start tailing all of the matched files if not already doing so.
	t.startTailing(matches)

	// Stop tailing any files which no longer exist
	toStopTailing := toStopTailing(matches, t.tails)
	t.stopTailingAndRemovePosition(toStopTailing)

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
			continue
		}
		if fi.IsDir() {
			level.Error(t.logger).Log("msg", "failed to tail file", "error", "file is a directory", "filename", p)
			continue
		}
		level.Debug(t.logger).Log("msg", "tailing new file", "filename", p)
		tailer, err := newTailer(t.logger, t.handler, t.positions, p)
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to start tailer", "error", err, "filename", p)
			continue
		}
		t.tails[p] = tailer
	}
}

// stopTailingAndRemovePosition will stop the tailer and remove the positions entry.
// Call this when a file no longer exists and you want to remove all traces of it.
func (t *FileTarget) stopTailingAndRemovePosition(ps []string) {
	for _, p := range ps {
		if tailer, ok := t.tails[p]; ok {
			tailer.stop()
			t.positions.Remove(tailer.path)
			delete(t.tails, p)
		}
		if h, ok := t.handler.(api.InstrumentedEntryHandler); ok {
			h.UnregisterLatencyMetric(model.LabelSet{model.LabelName(client.LatencyLabel): model.LabelValue(p)})
		}
	}
}

// pruneStoppedTailers removes any tailers which have stopped running from
// the list of active tailers. This allows them to be restarted if there were errors.
func (t *FileTarget) pruneStoppedTailers() {
	toRemove := make([]string, 0, len(t.tails))
	for k, t := range t.tails {
		if !t.isRunning() {
			toRemove = append(toRemove, k)
		}
	}
	for _, tr := range toRemove {
		delete(t.tails, tr)
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

func (t *FileTarget) reportSize(ms []string) {
	for _, m := range ms {
		// Ask the tailer to update the size if a tailer exists, this keeps position and size metrics in sync
		if tailer, ok := t.tails[m]; ok {
			err := tailer.markPositionAndSize()
			if err != nil {
				level.Warn(t.logger).Log("msg", "failed to get file size from tailer, ", "file", m, "error", err)
				return
			}
		} else {
			// Must be a new file, just directly read the size of it
			fi, err := os.Stat(m)
			if err != nil {
				// If the file was deleted between when the glob match and here,
				// we just ignore recording a size for it,
				// the tail code will also check if the file exists before creating a tailer.
				return
			}
			totalBytes.WithLabelValues(m).Set(float64(fi.Size()))
		}

	}
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
