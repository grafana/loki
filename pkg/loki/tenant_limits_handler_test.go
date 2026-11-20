package loki

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
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

func TestTenantLimitsHandlerWithAllowlist(t *testing.T) {
	limits := &validation.Limits{
		IngestionRateMB:        10.0,
		MaxLabelNameLength:     100,
		MaxQuerySeries:         1000,
		MaxLocalStreamsPerUser: 500,
		RejectOldSamples:       true,
		MaxLineSizeTruncate:    false,
		OTLPConfig: &push.OTLPConfig{
			ResourceAttributes: push.ResourceAttributesConfig{
				AttributesConfig: []push.AttributesConfig{
					{
						Action:     push.IndexLabel,
						Attributes: []string{"foo", "bar"},
					},
				},
			},
		},
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
			allowlist:      []string{"ingestion_rate_mb", "max_query_series", "otlp_config"},
			checkResponse: func(t *testing.T, body []byte) {
				var response map[string]any
				err := yaml.Unmarshal(body, &response)
				require.NoError(t, err)

				// Should only contain allowed fields
				assert.Contains(t, response, "ingestion_rate_mb")
				assert.Contains(t, response, "max_query_series")
				assert.Contains(t, response, "otlp_config")

				// Should NOT contain non-allowed fields
				assert.NotContains(t, response, "max_label_name_length")
				assert.NotContains(t, response, "max_streams_per_user")
				assert.NotContains(t, response, "reject_old_samples")
				assert.NotContains(t, response, "max_line_size_truncate")

				// Verify correct values for allowed fields
				assert.Equal(t, int(10), response["ingestion_rate_mb"])
				assert.Equal(t, 1000, response["max_query_series"])

				// Compare YAML representation of otlp_config
				actualOtlpConfigYAML, err := yaml.Marshal(response["otlp_config"])
				require.NoError(t, err)
				expectedOtlpConfigYAML, err := yaml.Marshal(limits.OTLPConfig)
				require.NoError(t, err)
				assert.YAMLEq(t, string(expectedOtlpConfigYAML), string(actualOtlpConfigYAML))
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
				assert.Contains(t, response, "otlp_config")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/config/tenant/v1/limits", nil)
			req = req.WithContext(user.InjectOrgID(req.Context(), tt.tenantID))

			loki := &Loki{
				TenantLimits: mockTenantLimits,
				Cfg: Config{
					TenantLimitsAllowPublish: tt.allowlist,
				},
			}

			handler := loki.tenantLimitsHandler(false)

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

func TestTenantLimitsHandlerWithContextTenant(t *testing.T) {
	limits := &validation.Limits{
		IngestionRateMB: 10.0,
	}
	mockTenantLimits := &mockTenantLimits{limits: limits}

	loki := &Loki{
		TenantLimits: mockTenantLimits,
	}

	t.Run("resolves tenant from request context when auth is disabled and header is omitted", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/loki/api/v1/drilldown-limits", nil)
		ctx := user.InjectOrgID(req.Context(), "test-tenant")
		req = req.WithContext(ctx)

		handler := loki.tenantLimitsHandler(true)
		w := httptest.NewRecorder()
		handler(w, req)

		resp := w.Result()
		require.Equal(t, 200, resp.StatusCode)

		var res DrilldownConfigResponse
		err := json.NewDecoder(resp.Body).Decode(&res)
		require.NoError(t, err)
		require.Equal(t, float64(10), res.Limits["ingestion_rate_mb"])
	})

	t.Run("returns unauthorized when tenant is absent from request context", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/loki/api/v1/drilldown-limits", nil)

		handler := loki.tenantLimitsHandler(true)
		w := httptest.NewRecorder()
		handler(w, req)

		resp := w.Result()
		require.Equal(t, 401, resp.StatusCode)
	})
}
