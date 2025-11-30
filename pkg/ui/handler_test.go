package ui

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

func TestFeaturesHandler_GoldfishWithNamespaces(t *testing.T) {
	t.Run("returns goldfish object with namespaces when enabled", func(t *testing.T) {
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable:         true,
					CellANamespace: "loki-ops-002",
					CellBNamespace: "loki-ops-003",
				},
			},
			logger: log.NewNopLogger(),
		}

		handler := service.featuresHandler()
		req := httptest.NewRequest("GET", "/api/v1/features", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]any
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Check that goldfish is an object with the expected structure
		goldfishData, ok := response["goldfish"].(map[string]interface{})
		require.True(t, ok, "goldfish should be an object")

		assert.Equal(t, true, goldfishData["enabled"])
		assert.Equal(t, "loki-ops-002", goldfishData["cellANamespace"])
		assert.Equal(t, "loki-ops-003", goldfishData["cellBNamespace"])
	})

	t.Run("returns goldfish object without namespaces when disabled", func(t *testing.T) {
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable:         false,
					CellANamespace: "loki-ops-002",
					CellBNamespace: "loki-ops-003",
				},
			},
			logger: log.NewNopLogger(),
		}

		handler := service.featuresHandler()
		req := httptest.NewRequest("GET", "/api/v1/features", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Check that goldfish is an object with only enabled: false
		goldfishData, ok := response["goldfish"].(map[string]interface{})
		require.True(t, ok, "goldfish should be an object")

		assert.Equal(t, false, goldfishData["enabled"])
		assert.Nil(t, goldfishData["cellANamespace"], "namespaces should not be included when disabled")
		assert.Nil(t, goldfishData["cellBNamespace"], "namespaces should not be included when disabled")
	})

	t.Run("returns goldfish object with enabled true but no namespaces when namespaces not configured", func(t *testing.T) {
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable: true,
					// No namespaces configured
				},
			},
			logger: log.NewNopLogger(),
		}

		handler := service.featuresHandler()
		req := httptest.NewRequest("GET", "/api/v1/features", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Check that goldfish is an object with only enabled: true
		goldfishData, ok := response["goldfish"].(map[string]interface{})
		require.True(t, ok, "goldfish should be an object")

		assert.Equal(t, true, goldfishData["enabled"])
		assert.Nil(t, goldfishData["cellANamespace"], "cellANamespace should be nil when not configured")
		assert.Nil(t, goldfishData["cellBNamespace"], "cellBNamespace should be nil when not configured")
	})
}

func TestGoldfishResultHandler(t *testing.T) {
	setup := func(enabled bool, storage goldfish.Storage, bucket objstore.InstrumentedBucket) *httptest.ResponseRecorder {
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable: enabled,
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
			goldfishBucket:  bucket,
		}

		handler := service.goldfishResultHandler(cellA)
		req := httptest.NewRequest("GET", "/api/v1/goldfish/results/test-id/cell-a", nil)
		req = mux.SetURLVars(req, map[string]string{"correlationId": "test-id"})
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		return rr
	}

	t.Run("returns error when goldfish is disabled", func(t *testing.T) {
		rr := setup(false, nil, nil)
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assert.Contains(t, rr.Body.String(), "goldfish feature is disabled")
	})

	t.Run("returns error when bucket client is not configured", func(t *testing.T) {
		rr := setup(true, nil, nil)
		assert.Equal(t, http.StatusNotImplemented, rr.Code)
		assert.Contains(t, rr.Body.String(), "result storage is not configured")
	})

	t.Run("returns 404 not found when correlation ID not found in database", func(t *testing.T) {
		storage := &mockStorage{
			getQueryFunc: func(_ context.Context, _ string) (*goldfish.QuerySample, error) {
				return nil, errors.New("not found")
			},
		}

		rr := setup(true, storage, &mockBucket{})
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assert.Contains(t, rr.Body.String(), "not found")
	})

	t.Run("returns 404 not found when result URI is empty", func(t *testing.T) {
		storage := &mockStorage{
			getQueryFunc: func(_ context.Context, correlationID string) (*goldfish.QuerySample, error) {
				return &goldfish.QuerySample{
					CorrelationID:  correlationID,
					CellAResultURI: "", // No result URI
				}, nil
			},
		}

		rr := setup(true, storage, &mockBucket{})
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assert.Contains(t, rr.Body.String(), "was not persisted to object storage")
	})

	t.Run("successfully fetches and decompresses gzipped result", func(t *testing.T) {
		originalData := []byte(`{"result": "compressed test data"}`)

		// Compress data with gzip
		var compressedBuf bytes.Buffer
		gzWriter := gzip.NewWriter(&compressedBuf)
		_, err := gzWriter.Write(originalData)
		require.NoError(t, err)
		require.NoError(t, gzWriter.Close())

		storage := &mockStorage{
			getQueryFunc: func(_ context.Context, correlationID string) (*goldfish.QuerySample, error) {
				return &goldfish.QuerySample{
					CorrelationID:          correlationID,
					CellAResultURI:         "s3://test-bucket/path/to/result.json.gz",
					CellAResultCompression: "gzip",
				}, nil
			},
		}

		bucket := &mockBucket{
			getFunc: func(_ context.Context, key string) (io.ReadCloser, error) {
				assert.Equal(t, "path/to/result.json.gz", key)
				return io.NopCloser(bytes.NewReader(compressedBuf.Bytes())), nil
			},
		}

		rr := setup(true, storage, bucket)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
		assert.JSONEq(t, string(originalData), rr.Body.String())
	})
}

func TestParseObjectKeyFromURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expectedKey string
		expectError bool
	}{
		{
			name:        "valid GCS URI",
			uri:         "gcs://my-bucket/path/to/object.json",
			expectedKey: "path/to/object.json",
			expectError: false,
		},
		{
			name:        "valid S3 URI",
			uri:         "s3://my-bucket/path/to/object.json.gz",
			expectedKey: "path/to/object.json.gz",
			expectError: false,
		},
		{
			name:        "valid URI with nested path",
			uri:         "gcs://my-bucket/prefix/2025/01/01/test-id/cell-a.json",
			expectedKey: "prefix/2025/01/01/test-id/cell-a.json",
			expectError: false,
		},
		{
			name:        "valid URI with URL-encoded characters in path",
			uri:         "gcs://my-bucket/path/with%20spaces/object.json",
			expectedKey: "path/with spaces/object.json", // url.Parse automatically decodes
			expectError: false,
		},
		{
			name:        "unsupported scheme http",
			uri:         "http://bucket/path",
			expectedKey: "",
			expectError: true,
		},
		{
			name:        "unsupported scheme https",
			uri:         "https://bucket/path",
			expectedKey: "",
			expectError: true,
		},
		{
			name:        "missing path",
			uri:         "gcs://my-bucket",
			expectedKey: "",
			expectError: true,
		},
		{
			name:        "missing path with trailing slash",
			uri:         "gcs://my-bucket/",
			expectedKey: "",
			expectError: true,
		},
		{
			name:        "empty URI",
			uri:         "",
			expectedKey: "",
			expectError: true,
		},
		{
			name:        "malformed URI",
			uri:         "not-a-uri",
			expectedKey: "",
			expectError: true,
		},
		{
			name:        "realistic example",
			uri:         "gcs://dev-us-central-0-loki-dev-005-goldfish-results/goldfish/results/2024/10/11/fc761f29-edad-4152-bedf-331d8cf2dbd5/cell-a.json.gz",
			expectedKey: "goldfish/results/2024/10/11/fc761f29-edad-4152-bedf-331d8cf2dbd5/cell-a.json.gz",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseObjectKeyFromURI(tt.uri)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedKey, key)
			}
		})
	}
}
