package uploader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

type Config struct {
	// SHAPrefixSize is the size of the SHA prefix used for splitting object storage keys
	SHAPrefixSize int
}

// RegisterFlagsWithPrefix registers flags with the given prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.SHAPrefixSize, prefix+"sha-prefix-size", 2, "The size of the SHA prefix to use for generating object storage keys for data objects.")
}

func (cfg *Config) Validate() error {
	if cfg.SHAPrefixSize <= 0 {
		return fmt.Errorf("SHAPrefixSize must be greater than 0")
	}
	return nil
}

type Uploader struct {
	SHAPrefixSize int
	bucket        objstore.Bucket
	tenantID      string
	metrics       *metrics
	logger        log.Logger
}

func New(cfg Config, bucket objstore.Bucket, tenantID string, logger log.Logger) *Uploader {
	metrics := newMetrics(cfg.SHAPrefixSize)

	return &Uploader{
		SHAPrefixSize: cfg.SHAPrefixSize,
		bucket:        bucket,
		tenantID:      tenantID,
		metrics:       metrics,
		logger:        logger,
	}
}

func (d *Uploader) RegisterMetrics(reg prometheus.Registerer) error {
	return d.metrics.register(reg)
}

func (d *Uploader) UnregisterMetrics(reg prometheus.Registerer) {
	d.metrics.unregister(reg)
}

// getKey determines the key in object storage to upload the object to, based on our path scheme.
func (d *Uploader) getKey(ctx context.Context, object *dataobj.Object) (string, error) {
	hash := sha256.New224()

	reader, err := object.Reader(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}

	var sumBytes [sha256.Size224]byte
	sum := hash.Sum(sumBytes[:0])
	sumStr := hex.EncodeToString(sum)

	return fmt.Sprintf("tenant-%s/objects/%s/%s", d.tenantID, sumStr[:d.SHAPrefixSize], sumStr[d.SHAPrefixSize:]), nil
}

// Upload uploads an object to the configured bucket and returns the key.
func (d *Uploader) Upload(ctx context.Context, object *dataobj.Object) (key string, err error) {
	start := time.Now()

	timer := prometheus.NewTimer(d.metrics.uploadTime)
	defer timer.ObserveDuration()

	objectPath, err := d.getKey(ctx, object)
	if err != nil {
		d.metrics.uploadFailures.Inc()
		d.metrics.uploadSize.WithLabelValues(statusFailure).Observe(float64(object.Size()))
		return "", fmt.Errorf("generating object key: %w", err)
	}

	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})

	size := object.Size()

	logger := log.With(d.logger, "key", objectPath, "size", size)

	for backoff.Ongoing() {
		level.Debug(logger).Log("msg", "attempting to upload dataobj to object storage", "attempt", backoff.NumRetries())

		err = func() error {
			reader, err := object.Reader(ctx)
			if err != nil {
				return err
			}
			defer reader.Close()
			return d.bucket.Upload(ctx, objectPath, reader)
		}()
		if err == nil {
			break
		}
		level.Warn(logger).Log("msg", "failed to upload dataobj to object storage; will retry", "err", err, "attempt", backoff.NumRetries())
		backoff.Wait()
	}

	d.metrics.uploadTotal.Inc()

	if err != nil {
		d.metrics.uploadFailures.Inc()
		d.metrics.uploadSize.WithLabelValues(statusFailure).Observe(float64(size))
		return "", fmt.Errorf("uploading object after %d retries: %w", backoff.NumRetries(), err)
	}

	d.metrics.uploadSize.WithLabelValues(statusSuccess).Observe(float64(size))
	level.Info(logger).Log("msg", "uploaded dataobj to object storage", "duration", time.Since(start))
	return objectPath, nil
}
