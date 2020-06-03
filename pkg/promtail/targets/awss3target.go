package targets

import (
	"flag"
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
	s3ReadBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "s3_read_bytes_total",
		Help:      "Number of bytes read.",
	}, []string{"path"})
	s3TotalBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "s3_file_bytes_total",
		Help:      "Number of bytes total.",
	}, []string{"path"})
	s3ReadLines = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "s3_read_lines_total",
		Help:      "Number of lines read.",
	}, []string{"path"})
	s3FilesActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "s3_files_active_total",
		Help:      "Number of active files.",
	})
	ls3LogLengthHistogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "s3_log_entries_bytes",
		Help:      "the total count of bytes",
		Buckets:   prometheus.ExponentialBuckets(16, 2, 8),
	}, []string{"path"})
)

// Config describes behavior for Target
type s3Config struct {
	SyncPeriod time.Duration `yaml:"sync_period"`
	Stdin      bool          `yaml:"stdin"`
}

// RegisterFlags register flags.
func (cfg *s3Config) RegisterFlags(flags *flag.FlagSet) {
	flags.DurationVar(&cfg.SyncPeriod, "target.sync-period", 10*time.Second, "Period to resync directories being watched and files being tailed.")
	flags.BoolVar(&cfg.Stdin, "stdin", false, "Set to true to pipe logs to promtail.")
}

// FileTarget describes a particular set of logs.
type S3Target struct {
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
func NewS3Target(logger log.Logger, handler api.EntryHandler, positions positions.Positions, path string, labels model.LabelSet, discoveredLabels model.LabelSet, targetConfig *Config) (*S3Target, error) {

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "filetarget.fsnotify.NewWatcher")
	}

	t := &S3Target{
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

func (t *S3Target) run() {
	defer func() {
		helpers.LogError("closing watcher", t.watcher.Close)
		for _, v := range t.tails {
			helpers.LogError("updating tailer last position", v.markPositionAndSize)
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
				matched, err := doublestar.Match(t.path, event.Name)
				if err != nil {
					level.Error(t.logger).Log("msg", "failed to match file", "error", err, "filename", event.Name)
					continue
				}
				if !matched {
					level.Debug(t.logger).Log("msg", "new file does not match glob", "filename", event.Name)
					continue
				}
				// t.startTailing([]string{event.Name})
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

func (t *S3Target) sync() error {

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
	// t.reportSize(matches)

	// Get the current unique set of dirs to watch.
	dirs := map[string]struct{}{}
	for _, p := range matches {
		dirs[filepath.Dir(p)] = struct{}{}
	}

	// Add any directories which are not already being watched.
	// toStartWatching := missing(t.watches, dirs)
	// t.startWatching(toStartWatching)

	// Remove any directories which no longer need watching.
	// toStopWatching := missing(dirs, t.watches)
	// t.stopWatching(toStopWatching)

	// fsnotify.Watcher doesn't allow us to see what is currently being watched so we have to track it ourselves.
	t.watches = dirs

	// Start tailing all of the matched files if not already doing so.
	// t.startTailing(matches)

	// Stop tailing any files which no longer exist
	// toStopTailing := toStopTailing(matches, t.tails)
	// t.stopTailing(toStopTailing)

	return nil
}
