package usagestats

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/util/build"
)

const (
	// File name for the cluster seed file.
	ClusterSeedFileName = "loki_cluster_seed.json"
	// attemptNumber how many times we will try to read a corrupted cluster seed before deleting it.
	attemptNumber = 4
	// seedKey is the key for the cluster seed to use with the kv store.
	seedKey = "usagestats_token"
)

var (
	reportCheckInterval = time.Minute
	reportInterval      = 4 * time.Hour

	stabilityCheckInterval   = 5 * time.Second
	stabilityMinimunRequired = 6
)

type Config struct {
	Enabled       bool   `yaml:"reporting_enabled"`
	Leader        bool   `yaml:"-"`
	UsageStatsURL string `yaml:"usage_stats_url"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "reporting.enabled", true, "Enable anonymous usage reporting.")
	f.StringVar(&cfg.UsageStatsURL, "reporting.usage-stats-url", usageStatsURL, "URL to which reports are sent")
}

type Reporter struct {
	logger       log.Logger
	objectClient client.ObjectClient
	reg          prometheus.Registerer

	services.Service

	conf       Config
	kvConfig   kv.Config
	cluster    *ClusterSeed
	lastReport time.Time
}

func NewReporter(config Config, kvConfig kv.Config, objectClient client.ObjectClient, logger log.Logger, reg prometheus.Registerer) (*Reporter, error) {
	if !config.Enabled {
		return nil, nil
	}
	r := &Reporter{
		logger:       logger,
		objectClient: objectClient,
		conf:         config,
		kvConfig:     kvConfig,
		reg:          reg,
	}
	r.Service = services.NewBasicService(nil, r.running, nil)
	return r, nil
}

func (rep *Reporter) initLeader(ctx context.Context) *ClusterSeed {
	kvClient, err := kv.NewClient(rep.kvConfig, JSONCodec, nil, rep.logger)
	if err != nil {
		level.Info(rep.logger).Log("msg", "failed to create kv client", "err", err)
		return nil
	}
	// Try to become leader via the kv client
	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: time.Second,
		MaxBackoff: time.Minute,
		MaxRetries: 0,
	})
	for backoff.Ongoing() {
		// create a new cluster seed
		seed := ClusterSeed{
			UID:               uuid.NewString(),
			PrometheusVersion: build.GetVersion(),
			CreatedAt:         time.Now(),
		}
		if err := kvClient.CAS(ctx, seedKey, func(in interface{}) (out interface{}, retry bool, err error) {
			// The key is already set, so we don't need to do anything
			if in != nil {
				if kvSeed, ok := in.(*ClusterSeed); ok && kvSeed != nil && kvSeed.UID != seed.UID {
					seed = *kvSeed
					return nil, false, nil
				}
			}
			return &seed, true, nil
		}); err != nil {
			level.Info(rep.logger).Log("msg", "failed to CAS cluster seed key", "err", err)
			continue
		}
		// ensure stability of the cluster seed
		stableSeed := ensureStableKey(ctx, kvClient, rep.logger)
		seed = *stableSeed
		// Fetch the remote cluster seed.
		remoteSeed, err := rep.fetchSeed(ctx,
			func(err error) bool {
				// we only want to retry if the error is not an object not found error
				return !rep.objectClient.IsObjectNotFoundErr(err)
			})
		if err != nil {
			if rep.objectClient.IsObjectNotFoundErr(err) {
				// we are the leader and we need to save the file.
				if err := rep.writeSeedFile(ctx, seed); err != nil {
					level.Info(rep.logger).Log("msg", "failed to CAS cluster seed key", "err", err)
					backoff.Wait()
					continue
				}
				return &seed
			}
			backoff.Wait()
			continue
		}
		return remoteSeed
	}
	return nil
}

// ensureStableKey ensures that the cluster seed is stable for at least 30seconds.
// This is required when using gossiping kv client like memberlist which will never have the same seed
// but will converge eventually.
func ensureStableKey(ctx context.Context, kvClient kv.Client, logger log.Logger) *ClusterSeed {
	var (
		previous    *ClusterSeed
		stableCount int
	)
	for {
		time.Sleep(stabilityCheckInterval)
		value, err := kvClient.Get(ctx, seedKey)
		if err != nil {
			level.Debug(logger).Log("msg", "failed to get cluster seed key for stability check", "err", err)
			continue
		}
		if seed, ok := value.(*ClusterSeed); ok && seed != nil {
			if previous == nil {
				previous = seed
				continue
			}
			if previous.UID != seed.UID {
				previous = seed
				stableCount = 0
				continue
			}
			stableCount++
			if stableCount > stabilityMinimunRequired {
				return seed
			}
		}
	}
}

func (rep *Reporter) init(ctx context.Context) {
	if rep.conf.Leader {
		rep.cluster = rep.initLeader(ctx)
		return
	}
	// follower only wait for the cluster seed to be set.
	// it will try forever to fetch the cluster seed.
	seed, _ := rep.fetchSeed(ctx, nil)
	rep.cluster = seed
}

// fetchSeed fetches the cluster seed from the object store and try until it succeeds.
// continueFn allow you to decide if we should continue retrying. Nil means always retry
func (rep *Reporter) fetchSeed(ctx context.Context, continueFn func(err error) bool) (*ClusterSeed, error) {
	var (
		backoff = backoff.New(ctx, backoff.Config{
			MinBackoff: time.Second,
			MaxBackoff: time.Minute,
			MaxRetries: 0,
		})
		readingErr = 0
	)
	for backoff.Ongoing() {
		seed, err := rep.readSeedFile(ctx)
		if err != nil {
			if !rep.objectClient.IsObjectNotFoundErr(err) {
				readingErr++
			}
			level.Debug(rep.logger).Log("msg", "failed to read cluster seed file", "err", err)
			if readingErr > attemptNumber {
				if err := rep.objectClient.DeleteObject(ctx, ClusterSeedFileName); err != nil {
					level.Error(rep.logger).Log("msg", "failed to delete corrupted cluster seed file, deleting it", "err", err)
				}
				readingErr = 0
			}
			if continueFn == nil || continueFn(err) {
				backoff.Wait()
				continue
			}
			return nil, err
		}
		return seed, nil
	}
	return nil, backoff.Err()
}

// readSeedFile reads the cluster seed file from the object store.
func (rep *Reporter) readSeedFile(ctx context.Context) (*ClusterSeed, error) {
	reader, _, err := rep.objectClient.GetObject(ctx, ClusterSeedFileName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			level.Error(rep.logger).Log("msg", "failed to close reader", "err", err)
		}
	}()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	seed, err := JSONCodec.Decode(data)
	if err != nil {
		return nil, err
	}
	return seed.(*ClusterSeed), nil
}

// writeSeedFile writes the cluster seed to the object store.
func (rep *Reporter) writeSeedFile(ctx context.Context, seed ClusterSeed) error {
	data, err := JSONCodec.Encode(seed)
	if err != nil {
		return err
	}
	return rep.objectClient.PutObject(ctx, ClusterSeedFileName, bytes.NewReader(data))
}

// running inits the reporter seed and start sending report for every interval
func (rep *Reporter) running(ctx context.Context) error {
	rep.init(ctx)

	if rep.cluster == nil {
		<-ctx.Done()
		if err := ctx.Err(); !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
	// check every minute if we should report.
	ticker := time.NewTicker(reportCheckInterval)
	defer ticker.Stop()

	// find  when to send the next report.
	next := nextReport(reportInterval, rep.cluster.CreatedAt, time.Now())
	if rep.lastReport.IsZero() {
		// if we never reported assumed it was the last interval.
		rep.lastReport = next.Add(-reportInterval)
	}
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			if !next.Equal(now) && now.Sub(rep.lastReport) < reportInterval {
				continue
			}
			level.Debug(rep.logger).Log("msg", "reporting cluster stats", "date", time.Now())
			if err := rep.reportUsage(ctx, next); err != nil {
				level.Info(rep.logger).Log("msg", "failed to report usage", "err", err)
				continue
			}
			rep.lastReport = next
			next = next.Add(reportInterval)
		case <-ctx.Done():
			if err := ctx.Err(); !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		}
	}
}

// reportUsage reports the usage to grafana.com.
func (rep *Reporter) reportUsage(ctx context.Context, interval time.Time) error {
	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: time.Second,
		MaxBackoff: 30 * time.Second,
		MaxRetries: 5,
	})
	var errs multierror.MultiError
	for backoff.Ongoing() {
		if err := sendReport(ctx, rep.cluster, interval, rep.conf.UsageStatsURL); err != nil {
			level.Info(rep.logger).Log("msg", "failed to send usage report", "retries", backoff.NumRetries(), "err", err)
			errs.Add(err)
			backoff.Wait()
			continue
		}
		level.Debug(rep.logger).Log("msg", "usage report sent with success")
		return nil
	}
	return errs.Err()
}

// nextReport compute the next report time based on the interval.
// The interval is based off the creation of the cluster seed to avoid all cluster reporting at the same time.
func nextReport(interval time.Duration, createdAt, now time.Time) time.Time {
	// createdAt * (x * interval ) >= now
	return createdAt.Add(time.Duration(math.Ceil(float64(now.Sub(createdAt))/float64(interval))) * interval)
}
