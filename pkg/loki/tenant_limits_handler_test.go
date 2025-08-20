package loki

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/validation"
)

type mockTenantLimits struct {
	limits *validation.Limits
}

func (m *mockTenantLimits) TenantLimits(userID string) *validation.Limits {
	if userID == "missing-tenant" {
		return nil
	}
	return m.limits
}

func (m *mockTenantLimits) AllByUserID() map[string]*validation.Limits {
	return map[string]*validation.Limits{"test-tenant": m.limits}
}

type mockOverrides struct {
	limits *validation.Limits
}

func (m *mockOverrides) DefaultLimits() *validation.Limits {
	return m.limits
}

func (m *mockOverrides) AllowStructuredMetadata() bool { return false }

func TestTenantLimitsHandlerWithAllowlist(t *testing.T) {
	limits := &validation.Limits{
		IngestionRateMB:        10.0,
		MaxLabelNameLength:     100,
		MaxQuerySeries:         1000,
		MaxLocalStreamsPerUser: 500,
		RejectOldSamples:       true,
		MaxLineSizeTruncate:    false,
	}

	mockTenantLimits := &mockTenantLimits{limits: limits}

	tests := []struct {
		name           string
		tenantID       string
		expectedStatus int
		allowlist      []string
		checkResponse  func(t *testing.T, body []byte)
	}{
		{
			name:           "successful tenant limits with allowlist filtering",
			tenantID:       "test-tenant",
			expectedStatus: 200,
			allowlist:      []string{"ingestion_rate_mb", "max_query_series"},
			checkResponse: func(t *testing.T, body []byte) {
				var response map[string]any
				err := yaml.Unmarshal(body, &response)
				require.NoError(t, err)

				// Should only contain allowed fields
				assert.Contains(t, response, "ingestion_rate_mb")
				assert.Contains(t, response, "max_query_series")

				// Should NOT contain non-allowed fields
				assert.NotContains(t, response, "max_label_name_length")
				assert.NotContains(t, response, "max_streams_per_user")
				assert.NotContains(t, response, "reject_old_samples")
				assert.NotContains(t, response, "max_line_size_truncate")

				// Verify correct values for allowed fields
				assert.Equal(t, int(10), response["ingestion_rate_mb"])
				assert.Equal(t, 1000, response["max_query_series"])
			},
		},
		{
			name:           "empty allowlist returns all fields",
			tenantID:       "test-tenant",
			expectedStatus: 200,
			allowlist:      []string{},
			checkResponse: func(t *testing.T, body []byte) {
				var response map[string]any
				err := yaml.Unmarshal(body, &response)
				require.NoError(t, err)

				// Should only contain all fields
				assert.Contains(t, response, "ingestion_rate_mb")
				assert.Contains(t, response, "max_query_series")
				assert.Contains(t, response, "max_label_name_length")
				assert.Contains(t, response, "max_streams_per_user")
				assert.Contains(t, response, "reject_old_samples")
				assert.Contains(t, response, "max_line_size_truncate")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/config/tenant/v1/limits", nil)
			req.Header.Set("X-Scope-OrgID", tt.tenantID)

			loki := &Loki{
				TenantLimits: mockTenantLimits,
				Cfg: Config{
					TenantLimitsAllowPublish: tt.allowlist,
				},
			}

			handler := loki.tenantLimitsHandler()

			w := httptest.NewRecorder()
			handler(w, req)

			resp := w.Result()
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			if tt.checkResponse != nil {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				tt.checkResponse(t, body)
			}
		})
	}
}

func TestTenantLimitsHandlerJSONResponse(t *testing.T) {
	// Test that tenantLimitsHandler returns JSON when Accept: application/json is sent

	limits := &validation.Limits{
		IngestionRateMB:        10.0,
		MaxLabelNameLength:     100,
		MaxQuerySeries:         1000,
		MaxLocalStreamsPerUser: 500,
	}

	mockTenantLimits := &mockTenantLimits{limits: limits}

	loki := &Loki{
		TenantLimits: mockTenantLimits,
		Cfg: Config{
			TenantLimitsAllowPublish: []string{}, // Empty allowlist = all fields
		},
	}

	req := httptest.NewRequest("GET", "/config/tenant/v1/limits", nil)
	req.Header.Set("X-Scope-OrgID", "test-tenant")
	req.Header.Set("Accept", "application/json")

	handler := loki.tenantLimitsHandler()
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()

	// Should return JSON with correct content-type
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Response should be valid JSON
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var jsonResponse map[string]any
	err = json.Unmarshal(body, &jsonResponse)
	require.NoError(t, err, "Response should be valid JSON")

	// Verify expected fields are present with JSON field names
	assert.Equal(t, float64(10), jsonResponse["ingestion_rate_mb"])
	assert.Equal(t, float64(100), jsonResponse["max_label_name_length"])
	assert.Equal(t, float64(1000), jsonResponse["max_query_series"])
	assert.Equal(t, float64(500), jsonResponse["max_streams_per_user"])
}

func TestTenantLimitsHandlerUnsupportedAcceptFallback(t *testing.T) {
	// Test that unsupported Accept headers fall back to YAML for tenant limits

	limits := &validation.Limits{
		IngestionRateMB: 10.0,
	}

	mockTenantLimits := &mockTenantLimits{limits: limits}

	loki := &Loki{
		TenantLimits: mockTenantLimits,
		Cfg: Config{
			TenantLimitsAllowPublish: []string{},
		},
	}

	unsupportedTypes := []string{
		"application/xml",
		"text/html",
		"*/*",
	}

	for _, contentType := range unsupportedTypes {
		t.Run(contentType, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/config/tenant/v1/limits", nil)
			req.Header.Set("X-Scope-OrgID", "test-tenant")
			req.Header.Set("Accept", contentType)

			handler := loki.tenantLimitsHandler()
			w := httptest.NewRecorder()
			handler(w, req)

			// Should return YAML (default behavior)
			assert.Equal(t, 200, w.Code)
			assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
			assert.Contains(t, w.Body.String(), "ingestion_rate_mb:") // YAML format
		})
	}
}
