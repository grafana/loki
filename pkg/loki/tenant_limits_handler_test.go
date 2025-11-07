package loki

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

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
			req.Header.Set("X-Scope-OrgID", tt.tenantID)

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
