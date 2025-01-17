package file

import (
	"flag"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/tail/watch"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

const (
	FilenameLabel = "filename"
)

var errFileTargetStopped = errors.New("File target is stopped")

// Config describes behavior for Target
type Config struct {
	SyncPeriod time.Duration `mapstructure:"sync_period" yaml:"sync_period"`
	Stdin      bool          `mapstructure:"stdin" yaml:"stdin"`
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

type WatchConfig struct {
	MinPollFrequency time.Duration `mapstructure:"min_poll_frequency" yaml:"min_poll_frequency"`
	MaxPollFrequency time.Duration `mapstructure:"max_poll_frequency" yaml:"max_poll_frequency"`
}

var DefaultWatchConig = WatchConfig{
	MinPollFrequency: 250 * time.Millisecond,
	MaxPollFrequency: 250 * time.Millisecond,
}

// RegisterFlags with prefix registers flags where every name is prefixed by
// prefix. If prefix is a non-empty string, prefix should end with a period.
func (cfg *WatchConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	d := DefaultWatchConig

	f.DurationVar(&cfg.MinPollFrequency, prefix+"min_poll_frequency", d.MinPollFrequency, "Minimum period to poll for file changes")
	f.DurationVar(&cfg.MaxPollFrequency, prefix+"max_poll_frequency", d.MaxPollFrequency, "Maximum period to poll for file changes")
}

// RegisterFlags register flags.
func (cfg *WatchConfig) RegisterFlags(flags *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", flags)
}

type fileTargetEventType string

const (
	fileTargetEventWatchStart fileTargetEventType = "WATCH_START"
	fileTargetEventWatchStop  fileTargetEventType = "WATCH_STOP"
)

type fileTargetEvent struct {
	path      string
	eventType fileTargetEventType
}

// FileTarget describes a particular set of logs.
// nolint:revive
type FileTarget struct {
	metrics *Metrics
	logger  log.Logger

	handler          api.EntryHandler
	positions        positions.Positions
	labels           model.LabelSet
	discoveredLabels model.LabelSet

	fileEventWatcher   chan fsnotify.Event
	targetEventHandler chan fileTargetEvent
	watches            map[string]struct{}
	watchesMutex       sync.Mutex
	path               string
	pathExclude        string
	quit               chan struct{}
	done               chan struct{}

	readers      map[string]Reader
	readersMutex sync.Mutex

	targetConfig *Config
	watchConfig  WatchConfig

	decompressCfg *scrapeconfig.DecompressionConfig

	encoding string
}

// NewFileTarget create a new FileTarget.
func NewFileTarget(
	metrics *Metrics,
	logger log.Logger,
	handler api.EntryHandler,
	positions positions.Positions,
	path string,
	pathExclude string,
	labels model.LabelSet,
	discoveredLabels model.LabelSet,
	targetConfig *Config,
	watchConfig WatchConfig,
	fileEventWatcher chan fsnotify.Event,
	targetEventHandler chan fileTargetEvent,
	encoding string,
	decompressCfg *scrapeconfig.DecompressionConfig,
) (*FileTarget, error) {
	t := &FileTarget{
		logger:             logger,
		metrics:            metrics,
		path:               path,
		pathExclude:        pathExclude,
		labels:             labels,
		discoveredLabels:   discoveredLabels,
		handler:            api.AddLabelsMiddleware(labels).Wrap(handler),
		positions:          positions,
		quit:               make(chan struct{}),
		done:               make(chan struct{}),
		readers:            map[string]Reader{},
		targetConfig:       targetConfig,
		watchConfig:        watchConfig,
		fileEventWatcher:   fileEventWatcher,
		targetEventHandler: targetEventHandler,
		encoding:           encoding,
		decompressCfg:      decompressCfg,
	}

	go t.run()
	return t, nil
}

// Ready if at least one file is being tailed
func (t *FileTarget) Ready() bool {
	return t.getReadersLen() > 0
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
	t.readersMutex.Lock()
	for fileName := range t.readers {
		files[fileName], _ = t.positions.Get(fileName)
	}
	t.readersMutex.Unlock()
	return files
}

func (t *FileTarget) run() {
	defer func() {
		t.readersMutex.Lock()
		for _, v := range t.readers {
			v.Stop()
		}
		t.readersMutex.Unlock()
		level.Info(t.logger).Log("msg", "filetarget: watcher closed, tailer stopped, positions saved", "path", t.path)
		close(t.done)
	}()

	err := t.sync()
	if err != nil {
		level.Error(t.logger).Log("msg", "error running sync function", "error", err)
	}

	ticker := time.NewTicker(t.targetConfig.SyncPeriod)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-t.fileEventWatcher:
			if !ok {
				// fileEventWatcher has been closed
				return
			}
			switch event.Op {
			case fsnotify.Create:
				t.startTailing([]string{event.Name})
			default:
				// No-op we only care about Create events
			}
		case <-ticker.C:
			err := t.sync()
			if errors.Is(err, errFileTargetStopped) {
				// This file target has been stopped.
				// This is normal and there is no need to log an error.
				return
			}
			if err != nil {
				level.Error(t.logger).Log("msg", "error running sync function", "error", err)
			}
		case <-t.quit:
			return
		}
	}
}

func (t *FileTarget) sync() error {
	var matches, matchesExcluded []string
	if fi, err := os.Stat(t.path); err == nil && !fi.IsDir() {
		// if the path points to a file that exists, then it we can skip the Glob search
		matches = []string{t.path}
	} else {
		// Gets current list of files to tail.
		matches, err = doublestar.FilepathGlob(t.path)

		if err != nil {
			return errors.Wrap(err, "filetarget.sync.filepath.Glob")
		}
	}

	if fi, err := os.Stat(t.pathExclude); err == nil && !fi.IsDir() {
		matchesExcluded = []string{t.pathExclude}
	} else {
		matchesExcluded, err = doublestar.FilepathGlob(t.pathExclude)

		if err != nil {
			return errors.Wrap(err, "filetarget.sync.filepathexclude.Glob")
		}
	}

	for i := 0; i < len(matchesExcluded); i++ {
		for j := 0; j < len(matches); j++ {
			if matchesExcluded[i] == matches[j] {
				// exclude this specific match
				matches = append(matches[:j], matches[j+1:]...)
			}
		}
	}

	if len(matches) == 0 {
		level.Debug(t.logger).Log("msg", "no files matched requested path, nothing will be tailed", "path", t.path, "pathExclude", t.pathExclude)
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
	t.watchesMutex.Lock()
	toStartWatching := missing(t.watches, dirs)
	t.watchesMutex.Unlock()
	err := t.startWatching(toStartWatching)
	if errors.Is(err, errFileTargetStopped) {
		return err
	}

	// Remove any directories which no longer need watching.
	t.watchesMutex.Lock()
	toStopWatching := missing(dirs, t.watches)
	t.watchesMutex.Unlock()

	err = t.stopWatching(toStopWatching)
	if errors.Is(err, errFileTargetStopped) {
		return err
	}

	// fsnotify.Watcher doesn't allow us to see what is currently being watched so we have to track it ourselves.
	t.watchesMutex.Lock()
	t.watches = dirs
	t.watchesMutex.Unlock()

	// Check if any running tailers have stopped because of errors and remove them from the running list
	// (They will be restarted in startTailing)
	t.pruneStoppedTailers()

	// Start tailing all of the matched files if not already doing so.
	t.startTailing(matches)

	// Stop tailing any files which no longer exist
	t.readersMutex.Lock()
	toStopTailing := toStopTailing(matches, t.readers)
	t.readersMutex.Unlock()
	t.stopTailingAndRemovePosition(toStopTailing)

	return nil
}

func (t *FileTarget) startWatching(dirs map[string]struct{}) error {
	for dir := range dirs {
		if _, ok := t.getWatch(dir); ok {
			continue
		}

		level.Info(t.logger).Log("msg", "watching new directory", "directory", dir)
		select {
		case <-t.quit:
			return errFileTargetStopped
		case t.targetEventHandler <- fileTargetEvent{
			path:      dir,
			eventType: fileTargetEventWatchStart,
		}:
		}
	}
	return nil
}

func (t *FileTarget) stopWatching(dirs map[string]struct{}) error {
	for dir := range dirs {
		if _, ok := t.getWatch(dir); !ok {
			continue
		}

		level.Info(t.logger).Log("msg", "removing directory from watcher", "directory", dir)
		select {
		case <-t.quit:
			return errFileTargetStopped
		case t.targetEventHandler <- fileTargetEvent{
			path:      dir,
			eventType: fileTargetEventWatchStop,
		}:
		}
	}
	return nil
}

func (t *FileTarget) startTailing(ps []string) {
	for _, p := range ps {
		if _, ok := t.getReader(p); ok {
			continue
		}

		fi, err := os.Stat(p)
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to tail file, stat failed", "error", err, "filename", p)
			t.metrics.totalBytes.DeleteLabelValues(p)
			continue
		}

		if fi.IsDir() {
			level.Info(t.logger).Log("msg", "failed to tail file", "error", "file is a directory", "filename", p)
			t.metrics.totalBytes.DeleteLabelValues(p)
			continue
		}

		if t.pathExclude != "" {
			matched, err := doublestar.Match(t.pathExclude, p)
			if err != nil {
				level.Error(t.logger).Log("msg", "ignoring file, exclude pattern match failed", "error", err, "filename", p, "pathExclude", t.pathExclude)
				t.metrics.totalBytes.DeleteLabelValues(p)
				continue
			}
			if matched {
				level.Info(t.logger).Log("msg", "ignoring file", "error", "file matches exclude pattern", "filename", p, "pathExclude", t.pathExclude)
				t.metrics.totalBytes.DeleteLabelValues(p)
				continue
			}
		}

		var reader Reader
		if t.decompressCfg != nil && t.decompressCfg.Enabled {
			level.Debug(t.logger).Log("msg", "reading from compressed file", "filename", p)
			decompressor, err := newDecompressor(t.metrics, t.logger, t.handler, t.positions, p, t.encoding, t.decompressCfg)
			if err != nil {
				level.Error(t.logger).Log("msg", "failed to start decompressor", "error", err, "filename", p)
				continue
			}
			reader = decompressor
		} else {
			watchOptions := watch.PollingFileWatcherOptions{
				MinPollFrequency: t.watchConfig.MinPollFrequency,
				MaxPollFrequency: t.watchConfig.MaxPollFrequency,
			}

			level.Debug(t.logger).Log("msg", "tailing new file", "filename", p)
			tailer, err := newTailer(t.metrics, t.logger, t.handler, t.positions, watchOptions, p, t.encoding)
			if err != nil {
				level.Error(t.logger).Log("msg", "failed to start tailer", "error", err, "filename", p)
				continue
			}
			reader = tailer
		}
		t.setReader(p, reader)
	}
}

// stopTailingAndRemovePosition will stop the tailer and remove the positions entry.
// Call this when a file no longer exists and you want to remove all traces of it.
func (t *FileTarget) stopTailingAndRemovePosition(ps []string) {
	for _, p := range ps {
		if reader, ok := t.getReader(p); ok {
			reader.Stop()
			t.positions.Remove(reader.Path())
			t.removeReader(p)
		}
	}
}

// pruneStoppedTailers removes any tailers which have stopped running from
// the list of active tailers. This allows them to be restarted if there were errors.
func (t *FileTarget) pruneStoppedTailers() {
	t.readersMutex.Lock()
	toRemove := make([]string, 0, len(t.readers))
	for k, t := range t.readers {
		if !t.IsRunning() {
			toRemove = append(toRemove, k)
		}
	}
	for _, tr := range toRemove {
		delete(t.readers, tr)
	}
	t.readersMutex.Unlock()
}

func (t *FileTarget) getReadersLen() int {
	t.readersMutex.Lock()
	defer t.readersMutex.Unlock()
	return len(t.readers)
}

func (t *FileTarget) getReader(val string) (Reader, bool) {
	t.readersMutex.Lock()
	defer t.readersMutex.Unlock()
	reader, ok := t.readers[val]
	return reader, ok
}

func (t *FileTarget) setReader(val string, reader Reader) {
	t.readersMutex.Lock()
	defer t.readersMutex.Unlock()
	t.readers[val] = reader
}

func (t *FileTarget) getWatch(val string) (struct{}, bool) {
	t.watchesMutex.Lock()
	defer t.watchesMutex.Unlock()
	fileTarget, ok := t.watches[val]
	return fileTarget, ok
}

func (t *FileTarget) removeReader(val string) {
	t.readersMutex.Lock()
	defer t.readersMutex.Unlock()
	delete(t.readers, val)
}

func (t *FileTarget) getWatchesLen() int {
	t.watchesMutex.Lock()
	defer t.watchesMutex.Unlock()
	return len(t.watches)
}

func toStopTailing(nt []string, et map[string]Reader) []string {
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
		if reader, ok := t.getReader(m); ok {
			err := reader.MarkPositionAndSize()
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
			t.metrics.totalBytes.WithLabelValues(m).Set(float64(fi.Size()))
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
