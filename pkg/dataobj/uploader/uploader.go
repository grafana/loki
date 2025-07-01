package uploader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
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
func (d *Uploader) getKey(object *bytes.Buffer) string {
	sum := sha256.Sum224(object.Bytes())
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("tenant-%s/objects/%s/%s", d.tenantID, sumStr[:d.SHAPrefixSize], sumStr[d.SHAPrefixSize:])
}

// Upload uploads an object to the configured bucket and returns the key.
func (d *Uploader) Upload(ctx context.Context, object *bytes.Buffer) (string, error) {
	start := time.Now()

	timer := prometheus.NewTimer(d.metrics.uploadTime)
	defer timer.ObserveDuration()

	objectPath := d.getKey(object)

	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})

	size := len(object.Bytes())

	var err error
	for backoff.Ongoing() {
		err = d.bucket.Upload(ctx, objectPath, bytes.NewReader(object.Bytes()))
		if err == nil {
			break
		}
		backoff.Wait()
	}

	d.metrics.uploadTotal.Inc()

	if err != nil {
		d.metrics.uploadFailures.Inc()
		d.metrics.uploadSize.WithLabelValues(statusFailure).Observe(float64(size))
		return "", fmt.Errorf("uploading object after %d retries: %w", backoff.NumRetries(), err)
	}

	d.metrics.uploadSize.WithLabelValues(statusSuccess).Observe(float64(size))
	level.Debug(d.logger).Log("msg", "uploaded dataobj to object storage", "key", objectPath, "size", size, "duration", time.Since(start))
	return objectPath, nil
}
