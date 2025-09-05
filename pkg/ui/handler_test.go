package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
