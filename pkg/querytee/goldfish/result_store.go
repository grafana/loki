package goldfish

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"

	bucketclient "github.com/grafana/loki/v3/pkg/storage/bucket"
)

// ResultStore handles persistence of raw query results to object storage.
type ResultStore interface {
	Store(ctx context.Context, payload []byte, opts StoreOptions) (*StoredResult, error)
	Close(ctx context.Context) error
}

// StoreOptions contains metadata required to persist a result payload.
type StoreOptions struct {
	CorrelationID string
	CellLabel     string
	BackendName   string
	TenantID      string
	QueryType     string
	Hash          string
	StatusCode    int
	Timestamp     time.Time
	ContentType   string
}

// StoredResult describes the object stored in the backing bucket.
type StoredResult struct {
	URI          string
	Size         int64
	OriginalSize int64
	Compression  string
}

type bucketResultStore struct {
	bucket      objstore.InstrumentedBucket
	bucketName  string
	prefix      string
	compression string
	backend     string
	logger      log.Logger
}

// NewResultStore creates a ResultStore based on configuration.
func NewResultStore(ctx context.Context, cfg ResultsStorageConfig, logger log.Logger) (ResultStore, error) {
	bucketClient, err := bucketclient.NewClient(ctx, cfg.Backend, cfg.Bucket, "goldfish-results", logger)
	if err != nil {
		return nil, fmt.Errorf("create bucket client: %w", err)
	}

	var bucketName string
	switch cfg.Backend {
	case "s3":
		bucketName = cfg.Bucket.S3.BucketName
	case "gcs":
		bucketName = cfg.Bucket.GCS.BucketName
	default:
		return nil, fmt.Errorf("unsupported backend: %s", cfg.Backend)
	}

	if cfg.Bucket.StoragePrefix != "" {
		bucketName = fmt.Sprintf("%s/%s", bucketName, strings.Trim(cfg.Bucket.StoragePrefix, "/"))
	}

	return &bucketResultStore{
		bucket:      bucketClient,
		bucketName:  bucketName,
		prefix:      strings.Trim(cfg.ObjectPrefix, "/"),
		compression: cfg.Compression,
		backend:     cfg.Backend,
		logger:      logger,
	}, nil
}

func (s *bucketResultStore) Store(ctx context.Context, payload []byte, opts StoreOptions) (*StoredResult, error) {
	encoded, err := encodePayload(payload, s.compression)
	if err != nil {
		return nil, err
	}

	key := buildObjectKey(s.prefix, opts.CorrelationID, opts.CellLabel, opts.Timestamp, s.compression)
	if err := s.bucket.Upload(ctx, key, bytes.NewReader(encoded)); err != nil {
		return nil, fmt.Errorf("upload object: %w", err)
	}

	// Best-effort metadata sidecar for quick inspection.
	if meta := buildMetadata(payload, opts, s.compression); meta != nil {
		metaKey := key + ".meta.json"
		if err := s.bucket.Upload(ctx, metaKey, bytes.NewReader(meta)); err != nil {
			level.Warn(s.logger).Log(
				"component", "goldfish-result-store",
				"msg", "failed to upload metadata object",
				"object", metaKey,
				"err", err,
			)
		}
	}

	uri := fmt.Sprintf("%s://%s/%s", s.backend, s.bucketName, key)

	level.Debug(s.logger).Log(
		"component", "goldfish-result-store",
		"object", key,
		"bucket", s.bucketName,
		"backend", s.backend,
		"uri", uri,
		"size_bytes", len(encoded),
	)

	return &StoredResult{
		URI:          uri,
		Size:         int64(len(encoded)),
		OriginalSize: int64(len(payload)),
		Compression:  s.compression,
	}, nil
}

func (s *bucketResultStore) Close(context.Context) error {
	return s.bucket.Close()
}

func buildObjectKey(prefix, correlationID, cell string, ts time.Time, compression string) string {
	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}

	datePath := fmt.Sprintf("%04d/%02d/%02d", ts.Year(), ts.Month(), ts.Day())
	safePrefix := strings.Trim(prefix, "/")
	safeCorrelation := strings.ReplaceAll(correlationID, "..", "--")
	safeCell := strings.ReplaceAll(cell, "..", "--")

	ext := ".json"
	if compression == ResultsCompressionGzip {
		ext += ".gz"
	}

	parts := []string{safePrefix, datePath, safeCorrelation, fmt.Sprintf("%s%s", safeCell, ext)}
	return path.Join(filterEmpty(parts)...)
}

func filterEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func encodePayload(data []byte, compression string) ([]byte, error) {
	switch strings.ToLower(compression) {
	case ResultsCompressionNone:
		return append([]byte(nil), data...), nil
	case ResultsCompressionGzip:
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(data); err != nil {
			return nil, fmt.Errorf("gzip write: %w", err)
		}
		if err := gz.Close(); err != nil {
			return nil, fmt.Errorf("gzip close: %w", err)
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unknown compression: %s", compression)
	}
}

func buildMetadata(payload []byte, opts StoreOptions, compression string) []byte {
	meta := map[string]interface{}{
		"correlation_id": opts.CorrelationID,
		"cell":           opts.CellLabel,
		"tenant":         opts.TenantID,
		"query_type":     opts.QueryType,
		"status_code":    opts.StatusCode,
		"hash_fnv32":     opts.Hash,
		"stored_at":      time.Now().UTC().Format(time.RFC3339),
		"compression":    compression,
		"original_bytes": len(payload),
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return nil
	}
	return b
}
