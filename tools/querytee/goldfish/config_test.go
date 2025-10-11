package goldfish

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
)

func TestResultsStorageValidationRequiresSQLBackend(t *testing.T) {
	cfg := Config{
		Enabled: true,
		ResultsStorage: ResultsStorageConfig{
			Enabled: true,
			Mode:    ResultsPersistenceModeMismatchOnly,
		},
	}

	err := cfg.Validate()
	if err == nil || err.Error() != "goldfish.results.enabled requires a SQL storage backend (cloudsql or rds)" {
		t.Fatalf("expected SQL backend requirement error, got %v", err)
	}
}

func TestResultsStorageValidationInfersGCS(t *testing.T) {
	cfg := Config{
		Enabled: true,
		StorageConfig: StorageConfig{
			Type:             "cloudsql",
			CloudSQLUser:     "user",
			CloudSQLDatabase: "db",
		},
		ResultsStorage: ResultsStorageConfig{
			Enabled: true,
			Bucket:  bucket.Config{},
		},
	}
	cfg.ResultsStorage.Bucket.GCS.BucketName = "my-bucket"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ResultsStorage.Backend != ResultsBackendGCS {
		t.Fatalf("expected backend to be inferred as gcs, got %s", cfg.ResultsStorage.Backend)
	}

	if cfg.ResultsStorage.Mode != ResultsPersistenceModeMismatchOnly {
		t.Fatalf("expected mismatch-only mode, got %s", cfg.ResultsStorage.Mode)
	}

	if cfg.ResultsStorage.Compression != ResultsCompressionGzip {
		t.Fatalf("expected default compression gzip, got %s", cfg.ResultsStorage.Compression)
	}

	if cfg.ResultsStorage.ObjectPrefix != defaultResultsObjectPrefix {
		t.Fatalf("expected default object prefix, got %s", cfg.ResultsStorage.ObjectPrefix)
	}
}

func TestResultsStorageValidationUnsupportedBackend(t *testing.T) {
	cfg := Config{
		Enabled: true,
		StorageConfig: StorageConfig{
			Type:             "cloudsql",
			CloudSQLUser:     "user",
			CloudSQLDatabase: "db",
		},
		ResultsStorage: ResultsStorageConfig{
			Enabled: true,
			Backend: "unsupported",
		},
	}

	err := cfg.Validate()
	if err == nil || err.Error() != "unsupported goldfish.results.backend: unsupported" {
		t.Fatalf("expected unsupported backend error, got %v", err)
	}
}

func TestResultsStorageValidationMode(t *testing.T) {
	cfg := Config{
		Enabled: true,
		StorageConfig: StorageConfig{
			Type:             "cloudsql",
			CloudSQLUser:     "user",
			CloudSQLDatabase: "db",
		},
		ResultsStorage: ResultsStorageConfig{
			Enabled: true,
			Mode:    "always",
		},
	}

	err := cfg.Validate()
	if err == nil || err.Error() != "unsupported goldfish.results.mode: always" {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}
}
