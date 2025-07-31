package uploader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
)

type MultiTenantUploader struct {
	SHAPrefixSize int
	bucket        objstore.Bucket
	metrics       *metrics
	logger        log.Logger
}

func NewMultiTenant(cfg Config, bucket objstore.Bucket, logger log.Logger) *MultiTenantUploader {
	metrics := newMetrics(cfg.SHAPrefixSize)

	return &MultiTenantUploader{
		SHAPrefixSize: cfg.SHAPrefixSize,
		bucket:        bucket,
		metrics:       metrics,
		logger:        logger,
	}
}

func (d *MultiTenantUploader) RegisterMetrics(reg prometheus.Registerer) error {
	return d.metrics.register(reg)
}

func (d *MultiTenantUploader) UnregisterMetrics(reg prometheus.Registerer) {
	d.metrics.unregister(reg)
}

// getKey determines the key in object storage to upload the object to, based on our path scheme.
func (d *MultiTenantUploader) getKey(object *bytes.Buffer) string {
	sum := sha256.Sum224(object.Bytes())
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("multi-tenant/objects/%s/%s", sumStr[:d.SHAPrefixSize], sumStr[d.SHAPrefixSize:])
}

// Upload uploads an object to the configured bucket and returns the key.
func (d *MultiTenantUploader) Upload(ctx context.Context, object *bytes.Buffer) (string, error) {
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

	logger := log.With(d.logger, "key", objectPath, "size", size)

	var err error
	for backoff.Ongoing() {
		level.Debug(logger).Log("msg", "attempting to upload dataobj to object storage", "attempt", backoff.NumRetries())
		err = d.bucket.Upload(ctx, objectPath, bytes.NewReader(object.Bytes()))
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
