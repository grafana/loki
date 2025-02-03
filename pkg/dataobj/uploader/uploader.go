package uploader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
)

type UploaderConfig struct {
	SHAPrefixSize int
}

type Uploader struct {
	SHAPrefixSize int
	bucket        objstore.Bucket
	tenantID      string
	metrics       *Metrics
}

func New(cfg UploaderConfig, bucket objstore.Bucket, tenantID string, reg prometheus.Registerer) *Uploader {
	metrics := NewMetrics(reg)

	return &Uploader{
		SHAPrefixSize: cfg.SHAPrefixSize,
		bucket:        bucket,
		tenantID:      tenantID,
		metrics:       metrics,
	}
}

func (d *Uploader) RegisterMetrics(reg prometheus.Registerer) error {
	return d.metrics.Register(reg)
}

func (d *Uploader) UnregisterMetrics(reg prometheus.Registerer) {
	d.metrics.Unregister(reg)
}

func (d *Uploader) getPath(object *bytes.Buffer) string {
	sum := sha256.Sum224(object.Bytes())
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("tenant-%s/objects/%s/%s", d.tenantID, sumStr[:d.SHAPrefixSize], sumStr[d.SHAPrefixSize:])
}

func (d *Uploader) Upload(ctx context.Context, object *bytes.Buffer) (string, error) {
	timer := prometheus.NewTimer(d.metrics.UploadTime)
	defer timer.ObserveDuration()

	objectPath := d.getPath(object)

	if err := d.bucket.Upload(ctx, objectPath, bytes.NewReader(object.Bytes())); err != nil {
		return "", fmt.Errorf("uploading object: %w", err)
	}

	return objectPath, nil
}
