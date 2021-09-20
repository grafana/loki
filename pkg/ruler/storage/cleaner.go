package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/grafana/agent/pkg/metrics/wal"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	promwal "github.com/prometheus/prometheus/tsdb/wal"
)

// Default settings for the WAL cleaner.
const (
	DefaultCleanupAge    = 12 * time.Hour
	DefaultCleanupPeriod = 30 * time.Minute
)

var (
	discoveryError = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_prometheus_cleaner_storage_error_total",
			Help: "Errors encountered discovering local storage paths",
		},
		[]string{"storage"},
	)

	segmentError = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_prometheus_cleaner_segment_error_total",
			Help: "Errors encountered finding most recent WAL segments",
		},
		[]string{"storage"},
	)

	managedStorage = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "agent_prometheus_cleaner_managed_storage",
			Help: "Number of storage directories associated with managed instances",
		},
	)

	abandonedStorage = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "agent_prometheus_cleaner_abandoned_storage",
			Help: "Number of storage directories not associated with any managed instance",
		},
	)

	cleanupRunsSuccess = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "agent_prometheus_cleaner_success_total",
			Help: "Number of successfully removed abandoned WALs",
		},
	)

	cleanupRunsErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "agent_prometheus_cleaner_errors_total",
			Help: "Number of errors removing abandoned WALs",
		},
	)

	cleanupTimes = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name: "agent_prometheus_cleaner_cleanup_seconds",
			Help: "Time spent performing each periodic WAL cleanup",
		},
	)
)

// lastModifiedFunc gets the last modified time of the most recent segment of a WAL
type lastModifiedFunc func(path string) (time.Time, error)

func lastModified(path string) (time.Time, error) {
	existing, err := promwal.Open(nil, path)
	if err != nil {
		return time.Time{}, err
	}

	// We don't care if there are errors closing the abandoned WAL
	defer func() { _ = existing.Close() }()

	_, last, err := promwal.Segments(existing.Dir())
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to open WAL: %w", err)
	}

	if last == -1 {
		return time.Time{}, fmt.Errorf("unable to determine most recent segment for %s", path)
	}

	// full path to the most recent segment in this WAL
	lastSegment := promwal.SegmentName(path, last)
	segmentFile, err := os.Stat(lastSegment)
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to determine mtime for %s segment: %w", lastSegment, err)
	}

	return segmentFile.ModTime(), nil
}

// WALCleaner periodically checks for Write Ahead Logs (WALs) that are not associated
// with any active instance.ManagedInstance and have not been written to in some configured
// amount of time and deletes them.
type WALCleaner struct {
	logger          log.Logger
	instanceManager instance.Manager
	walDirectory    string
	walLastModified lastModifiedFunc
	minAge          time.Duration
	period          time.Duration
	done            chan bool
}

// NewWALCleaner creates a new cleaner that looks for abandoned WALs in the given
// directory and removes them if they haven't been modified in over minAge. Starts
// a goroutine to periodically run the cleanup method in a loop
func NewWALCleaner(logger log.Logger, manager instance.Manager, walDirectory string, minAge time.Duration, period time.Duration) *WALCleaner {
	c := &WALCleaner{
		logger:          log.With(logger, "component", "cleaner"),
		instanceManager: manager,
		walDirectory:    filepath.Clean(walDirectory),
		walLastModified: lastModified,
		minAge:          DefaultCleanupAge,
		period:          DefaultCleanupPeriod,
		done:            make(chan bool),
	}

	if minAge > 0 {
		c.minAge = minAge
	}

	// We allow a period of 0 here because '0' means "don't run the task". This
	// is handled by not running a ticker at all in the run method.
	if period >= 0 {
		c.period = period
	}

	go c.run()
	return c
}

// getManagedStorage gets storage directories used for each ManagedInstance
func (c *WALCleaner) getManagedStorage(instances map[string]instance.ManagedInstance) map[string]bool {
	out := make(map[string]bool)

	for _, inst := range instances {
		out[inst.StorageDirectory()] = true
	}

	return out
}

// getAllStorage gets all storage directories under walDirectory
func (c *WALCleaner) getAllStorage() []string {
	var out []string

	_ = filepath.Walk(c.walDirectory, func(p string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			// The root WAL directory doesn't exist. Maybe this Agent isn't responsible for any
			// instances yet. Log at debug since this isn't a big deal. We'll just try to crawl
			// the direction again on the next periodic run.
			level.Debug(c.logger).Log("msg", "WAL storage path does not exist", "path", p, "err", err)
		} else if err != nil {
			// Just log any errors traversing the WAL directory. This will potentially result
			// in a WAL (that has incorrect permissions or some similar problem) not being cleaned
			// up. This is  better than preventing *all* other WALs from being cleaned up.
			discoveryError.WithLabelValues(p).Inc()
			level.Warn(c.logger).Log("msg", "unable to traverse WAL storage path", "path", p, "err", err)
		} else if info.IsDir() && filepath.Dir(p) == c.walDirectory {
			// Single level below the root are instance storage directories (including WALs)
			out = append(out, p)
		}

		return nil
	})

	return out
}

// getAbandonedStorage gets the full path of storage directories that aren't associated with
// an active instance  and haven't been written to within a configured duration (usually several
// hours or more).
func (c *WALCleaner) getAbandonedStorage(all []string, managed map[string]bool, now time.Time) []string {
	var out []string

	for _, dir := range all {
		if managed[dir] {
			level.Debug(c.logger).Log("msg", "active WAL", "name", dir)
			continue
		}

		walDir := wal.SubDirectory(dir)
		mtime, err := c.walLastModified(walDir)
		if err != nil {
			segmentError.WithLabelValues(dir).Inc()
			level.Warn(c.logger).Log("msg", "unable to find segment mtime of WAL", "name", dir, "err", err)
			continue
		}

		diff := now.Sub(mtime)
		if diff > c.minAge {
			// The last segment for this WAL was modified more then $minAge (positive number of hours)
			// in the past. This makes it a candidate for deletion since it's also not associated with
			// any Instances this agent knows about.
			out = append(out, dir)
		}

		level.Debug(c.logger).Log("msg", "abandoned WAL", "name", dir, "mtime", mtime, "diff", diff)
	}

	return out
}

// run cleans up abandoned WALs (if period != 0) in a loop periodically until stopped
func (c *WALCleaner) run() {
	// A period of 0 means don't run a cleanup task
	if c.period == 0 {
		return
	}

	ticker := time.NewTicker(c.period)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			level.Debug(c.logger).Log("msg", "stopping cleaner...")
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// cleanup removes any abandoned and unused WAL directories. Note that it shouldn't be
// necessary to call this method explicitly in most cases since it will be run periodically
// in a goroutine (started when WALCleaner is created).
func (c *WALCleaner) cleanup() {
	start := time.Now()
	all := c.getAllStorage()
	managed := c.getManagedStorage(c.instanceManager.ListInstances())
	abandoned := c.getAbandonedStorage(all, managed, time.Now())

	managedStorage.Set(float64(len(managed)))
	abandonedStorage.Set(float64(len(abandoned)))

	for _, a := range abandoned {
		level.Info(c.logger).Log("msg", "deleting abandoned WAL", "name", a)
		err := os.RemoveAll(a)
		if err != nil {
			level.Error(c.logger).Log("msg", "failed to delete abandoned WAL", "name", a, "err", err)
			cleanupRunsErrors.Inc()
		} else {
			cleanupRunsSuccess.Inc()
		}
	}

	cleanupTimes.Observe(time.Since(start).Seconds())
}

// Stop the cleaner and any background tasks running
func (c *WALCleaner) Stop() {
	close(c.done)
}
