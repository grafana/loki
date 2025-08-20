package loki

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/pattern"
	"github.com/grafana/loki/v3/pkg/validation"
)

type diffConfigMock struct {
	MyInt          int          `yaml:"my_int" json:"my_int"`
	MyFloat        float64      `yaml:"my_float" json:"my_float"`
	MySlice        []string     `yaml:"my_slice" json:"my_slice"`
	IgnoredField   func() error `yaml:"-" json:"-"`
	MyNestedStruct struct {
		MyString      string   `yaml:"my_string" json:"my_string"`
		MyBool        bool     `yaml:"my_bool" json:"my_bool"`
		MyEmptyStruct struct{} `yaml:"my_empty_struct" json:"my_empty_struct"`
	} `yaml:"my_nested_struct" json:"my_nested_struct"`
}

func newDefaultDiffConfigMock() *diffConfigMock {
	c := &diffConfigMock{
		MyInt:        666,
		MyFloat:      6.66,
		MySlice:      []string{"value1", "value2"},
		IgnoredField: func() error { return nil },
	}
	c.MyNestedStruct.MyString = "string1"
	return c
}

func TestConfigDiffHandler(t *testing.T) {
	for _, tc := range []struct {
		name               string
		expectedStatusCode int
		expectedBody       string
		actualConfig       func() any
	}{
		{
			name:               "no config parameters overridden",
			expectedStatusCode: 200,
			expectedBody:       "{}\n",
		},
		{
			name: "slice changed",
			actualConfig: func() any {
				c := newDefaultDiffConfigMock()
				c.MySlice = append(c.MySlice, "value3")
				return c
			},
			expectedStatusCode: 200,
			expectedBody: "my_slice:\n" +
				"- value1\n" +
				"- value2\n" +
				"- value3\n",
		},
		{
			name: "string in nested struct changed",
			actualConfig: func() any {
				c := newDefaultDiffConfigMock()
				c.MyNestedStruct.MyString = "string2"
				return c
			},
			expectedStatusCode: 200,
			expectedBody: "my_nested_struct:\n" +
				"  my_string: string2\n",
		},
		{
			name: "bool in nested struct changed",
			actualConfig: func() any {
				c := newDefaultDiffConfigMock()
				c.MyNestedStruct.MyBool = true
				return c
			},
			expectedStatusCode: 200,
			expectedBody: "my_nested_struct:\n" +
				"  my_bool: true\n",
		},
		{
			name: "test invalid input",
			actualConfig: func() any {
				c := "x"
				return &c
			},
			expectedStatusCode: 500,
			expectedBody: "yaml: unmarshal errors:\n" +
				"  line 1: cannot unmarshal !!str `x` into map[interface {}]interface {}\n",
		},
	} {
		defaultCfg := newDefaultDiffConfigMock()
		t.Run(tc.name, func(t *testing.T) {

			var actualCfg any
			if tc.actualConfig != nil {
				actualCfg = tc.actualConfig()
			} else {
				actualCfg = newDefaultDiffConfigMock()
			}

			req := httptest.NewRequest("GET", "http://test.com/config?mode=diff", nil)
			w := httptest.NewRecorder()

			h := configHandler(actualCfg, defaultCfg)
			h(w, req)
			resp := w.Result()
			assert.Equal(t, tc.expectedStatusCode, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedBody, string(body))
		})
	}

}

func TestFunctionFieldJSONMarshaling(t *testing.T) {
	// Demonstrate the actual problem: function fields can't be marshaled to JSON

	type StructWithFunc struct {
		Name     string
		Callback func() error // This causes JSON marshaling to fail
	}

	s := StructWithFunc{
		Name:     "test",
		Callback: func() error { return nil },
	}

	//nolint:staticcheck // SA1026: Intentionally testing marshaling of unsupported function type
	_, err := json.Marshal(s)
	assert.Error(t, err, "Should fail to marshal struct with function field")
	assert.Contains(t, err.Error(), "unsupported type")

	// But if we tag it with json:"-", it works
	type StructWithIgnoredFunc struct {
		Name     string
		Callback func() error `json:"-"` // This is ignored during JSON marshaling
	}

	s2 := StructWithIgnoredFunc{
		Name:     "test",
		Callback: func() error { return nil },
	}

	data, err := json.Marshal(s2)
	assert.NoError(t, err, "Should succeed when function field is ignored")

	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err, "Should succeed unmarshaling valid JSON")
	assert.Equal(t, "test", result["Name"])
	assert.NotContains(t, result, "Callback")
}

func TestLimitsDirectJSONMarshaling(t *testing.T) {
	// Test that validation.Limits can be directly marshaled to JSON
	// (it has proper json tags)
	limits := &validation.Limits{
		IngestionRateMB:    10.0,
		MaxLabelNameLength: 100,
		MaxQuerySeries:     1000,
	}

	// This should work directly without the map conversion
	data, err := json.Marshal(limits)
	require.NoError(t, err, "Limits should be directly marshalable to JSON")

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	// Verify the JSON field names (from json tags) are used
	assert.Equal(t, float64(10), result["ingestion_rate_mb"])
	assert.Equal(t, float64(100), result["max_label_name_length"])
	assert.Equal(t, float64(1000), result["max_query_series"])
}

// mockCombinedLimits embeds validation.Overrides to implement CombinedLimits
type mockCombinedLimits struct {
	*validation.Overrides
}

func TestDrilldownConfigOverridesFallback(t *testing.T) {
	defaultLimits := &validation.Limits{
		IngestionRateMB:    5.0,
		MaxQuerySeries:     500,
		MaxLabelNameLength: 50,
	}

	// Create a mock TenantLimits that returns nil for all tenants
	mockTenantLimits := &mockTenantLimits{
		limits: nil, // This will make TenantLimits return nil
	}

	// Create a real Overrides with default limits
	overrides, _ := validation.NewOverrides(*defaultLimits, nil)
	mockOverridesWithDefaults := &mockCombinedLimits{
		Overrides: overrides,
	}

	loki := &Loki{
		TenantLimits: mockTenantLimits,
		Overrides:    mockOverridesWithDefaults,
		Cfg: Config{
			TenantLimitsAllowPublish: []string{},
			Pattern: pattern.Config{
				Enabled: false,
			},
		},
	}

	handler := loki.drilldownConfigHandler()

	req := httptest.NewRequest("GET", "/loki/api/v1/config", nil)
	req.Header.Set("X-Scope-OrgID", "unknown-tenant")

	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should return 200 with default limits from Overrides
	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var response DrilldownConfigResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Should have the default limits from Overrides
	assert.Equal(t, 5.0, response.Limits["ingestion_rate_mb"])
	assert.Equal(t, float64(500), response.Limits["max_query_series"])
	assert.Equal(t, float64(50), response.Limits["max_label_name_length"])
}

func TestDrilldownConfigTenantLimitsSource(t *testing.T) {
	// Test the different sources for tenant limits
	// In production, defaults ALWAYS come from Overrides.DefaultLimits()
	// TenantLimits only provides per-tenant overrides or nil

	perTenantLimits := &validation.Limits{
		IngestionRateMB:    20.0,
		MaxQuerySeries:     2000,
		MaxLabelNameLength: 200,
	}

	defaultLimits := &validation.Limits{
		IngestionRateMB:    10.0,
		MaxQuerySeries:     1000,
		MaxLabelNameLength: 100,
	}

	testCases := []struct {
		name             string
		tenantID         string
		tenantLimits     validation.TenantLimits
		overrides        *mockCombinedLimits
		expectedRateMB   float64
		expectedSeries   float64
		expectedLabelLen float64
		expectedStatus   int
	}{
		{
			name:     "tenant has specific limits configured via TenantLimits",
			tenantID: "tenant-with-config",
			tenantLimits: &mockTenantLimits{
				limits: perTenantLimits, // This tenant has specific limits from runtime config
			},
			overrides:        nil, // Don't need Overrides since tenant has limits
			expectedRateMB:   20.0,
			expectedSeries:   float64(2000),
			expectedLabelLen: float64(200),
			expectedStatus:   200,
		},
		{
			name:     "no per-tenant limits - uses defaults from Overrides",
			tenantID: "tenant-without-config",
			tenantLimits: &mockTenantLimits{
				limits: nil, // TenantLimits returns nil for this tenant (no runtime config)
			},
			overrides: func() *mockCombinedLimits {
				o, _ := validation.NewOverrides(*defaultLimits, nil)
				return &mockCombinedLimits{Overrides: o}
			}(),
			expectedRateMB:   10.0,
			expectedSeries:   float64(1000),
			expectedLabelLen: float64(100),
			expectedStatus:   200,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			loki := &Loki{
				TenantLimits: tc.tenantLimits,
				Overrides:    tc.overrides,
				Cfg: Config{
					TenantLimitsAllowPublish: []string{}, // Empty allowlist = all fields
					Pattern: pattern.Config{
						Enabled: false,
					},
				},
			}

			handler := loki.drilldownConfigHandler()

			req := httptest.NewRequest("GET", "/loki/api/v1/config", nil)
			req.Header.Set("X-Scope-OrgID", tc.tenantID)

			w := httptest.NewRecorder()
			handler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			require.Equal(t, tc.expectedStatus, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var response DrilldownConfigResponse
			err = json.Unmarshal(body, &response)
			require.NoError(t, err)

			// Verify the correct limits are returned
			assert.Equal(t, tc.expectedRateMB, response.Limits["ingestion_rate_mb"])
			assert.Equal(t, tc.expectedSeries, response.Limits["max_query_series"])
			assert.Equal(t, tc.expectedLabelLen, response.Limits["max_label_name_length"])
		})
	}
}

// mockTenantLimitsWithDefaults allows us to simulate different tenants having different limits
// and also acts as Overrides to provide default limits
type mockTenantLimitsWithDefaults struct {
	tenantLimits  map[string]*validation.Limits
	defaultLimits *validation.Limits
}

func (m *mockTenantLimitsWithDefaults) TenantLimits(userID string) *validation.Limits {
	// Return per-tenant limits if they exist, otherwise return defaults
	if limits, ok := m.tenantLimits[userID]; ok {
		return limits
	}
	// For the test scenarios, we want to simulate that some tenants get defaults
	// through the TenantLimits interface rather than through Overrides
	if len(m.tenantLimits) == 0 {
		// Case 1: No runtime config at all, return defaults
		return m.defaultLimits
	}
	// Case 2: Runtime config exists but not for this tenant, return defaults
	return m.defaultLimits
}

func (m *mockTenantLimitsWithDefaults) AllByUserID() map[string]*validation.Limits {
	return m.tenantLimits
}

func (m *mockTenantLimitsWithDefaults) DefaultLimits() *validation.Limits {
	return m.defaultLimits
}

func (m *mockTenantLimitsWithDefaults) AllowStructuredMetadata(userID string) bool {
	return false
}

func TestDrilldownConfigResponseStructure(t *testing.T) {
	// Test that the drilldown config handler returns the filtered limits as JSON

	limits := &validation.Limits{
		IngestionRateMB:         10.5,
		IngestionBurstSizeMB:    15.0,
		MaxLineSizeTruncate:     true,
		MaxLineSize:             256,
		MaxLabelNameLength:      100,
		MaxLabelValueLength:     500,
		MaxLabelNamesPerSeries:  30,
		MaxQuerySeries:          1000,
		MaxEntriesLimitPerQuery: 5000,
		QueryTimeout:            model.Duration(60 * time.Second),
		MaxLocalStreamsPerUser:  500,
		MaxGlobalStreamsPerUser: 1000,
		RetentionPeriod:         model.Duration(24 * time.Hour),
		MaxQueryParallelism:     32,
	}

	mockTenantLimits := &mockTenantLimits{limits: limits}

	loki := &Loki{
		TenantLimits: mockTenantLimits,
		Cfg: Config{
			TenantLimitsAllowPublish: []string{}, // Empty allowlist = all fields
			Pattern: pattern.Config{
				Enabled: true, // Pattern ingester is enabled
			},
		},
	}

	handler := loki.drilldownConfigHandler()

	req := httptest.NewRequest("GET", "/loki/api/v1/config", nil)
	req.Header.Set("X-Scope-OrgID", "test-tenant")

	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should return 200 OK
	require.Equal(t, 200, resp.StatusCode)

	// Should return JSON by default
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Parse response
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var response DrilldownConfigResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err, "Response should be valid JSON")

	// Response should have a limits field containing the filtered limits
	require.NotNil(t, response.Limits)

	// Check a few key fields to verify the limits were included
	assert.Equal(t, float64(10.5), response.Limits["ingestion_rate_mb"])
	assert.Equal(t, float64(15), response.Limits["ingestion_burst_size_mb"])
	assert.Equal(t, float64(100), response.Limits["max_label_name_length"])
	assert.Equal(t, float64(1000), response.Limits["max_query_series"])
	assert.Equal(t, float64(500), response.Limits["max_streams_per_user"])

	// Check pattern ingester enabled field
	assert.Equal(t, true, response.PatternIngesterEnabled)

	// Check version field is present (we don't check the exact value as it may vary)
	assert.NotEmpty(t, response.Version)
}

func TestDrilldownConfigPatternIngesterDisabled(t *testing.T) {
	// Test that pattern ingester enabled field reflects the config

	limits := &validation.Limits{
		IngestionRateMB: 10.5,
		MaxQuerySeries:  1000,
	}

	mockTenantLimits := &mockTenantLimits{limits: limits}

	loki := &Loki{
		TenantLimits: mockTenantLimits,
		Cfg: Config{
			TenantLimitsAllowPublish: []string{},
			Pattern: pattern.Config{
				Enabled: false, // Pattern ingester is disabled
			},
		},
	}

	handler := loki.drilldownConfigHandler()

	req := httptest.NewRequest("GET", "/loki/api/v1/config", nil)
	req.Header.Set("X-Scope-OrgID", "test-tenant")

	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	require.Equal(t, 200, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var response DrilldownConfigResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Pattern ingester should be disabled
	assert.Equal(t, false, response.PatternIngesterEnabled)

	// Version should still be present
	assert.NotEmpty(t, response.Version)
}

func TestDrilldownConfigWithAllowlist(t *testing.T) {
	// Test that the drilldown config handler respects the allowlist filter

	limits := &validation.Limits{
		IngestionRateMB:        10.5,
		MaxQuerySeries:         1000,
		MaxLocalStreamsPerUser: 500,
		MaxLabelNameLength:     100,
	}

	mockTenantLimits := &mockTenantLimits{limits: limits}

	loki := &Loki{
		TenantLimits: mockTenantLimits,
		Cfg: Config{
			// Only allow specific fields
			TenantLimitsAllowPublish: []string{"ingestion_rate_mb", "max_query_series"},
		},
	}

	handler := loki.drilldownConfigHandler()

	req := httptest.NewRequest("GET", "/loki/api/v1/config", nil)
	req.Header.Set("X-Scope-OrgID", "test-tenant")

	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	require.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var response DrilldownConfigResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Response should have a limits field
	require.NotNil(t, response.Limits)

	// Should only contain allowed fields
	assert.Contains(t, response.Limits, "ingestion_rate_mb")
	assert.Contains(t, response.Limits, "max_query_series")

	// Should NOT contain filtered out fields
	assert.NotContains(t, response.Limits, "max_streams_per_user")
	assert.NotContains(t, response.Limits, "max_label_name_length")

	// Verify values for allowed fields
	assert.Equal(t, float64(10.5), response.Limits["ingestion_rate_mb"])
	assert.Equal(t, float64(1000), response.Limits["max_query_series"])
}

func TestFilterLimitFieldsReturnsJSONMap(t *testing.T) {
	// Test that filterLimitFields returns a proper map[string]any

	limits := &validation.Limits{
		IngestionRateMB:    10.5,
		MaxQuerySeries:     1000,
		MaxLabelNameLength: 100,
	}

	testCases := []struct {
		name      string
		allowlist []string
		verify    func(t *testing.T, result map[string]any)
	}{
		{
			name:      "empty allowlist returns all fields as map",
			allowlist: []string{},
			verify: func(t *testing.T, result map[string]any) {
				assert.Equal(t, 10.5, result["ingestion_rate_mb"])
				assert.Equal(t, float64(1000), result["max_query_series"])
				assert.Equal(t, float64(100), result["max_label_name_length"])
			},
		},
		{
			name:      "allowlist filters fields correctly",
			allowlist: []string{"ingestion_rate_mb", "max_query_series"},
			verify: func(t *testing.T, result map[string]any) {
				assert.Equal(t, 10.5, result["ingestion_rate_mb"])
				assert.Equal(t, float64(1000), result["max_query_series"])
				assert.NotContains(t, result, "max_label_name_length")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := filterLimitFields(limits, tc.allowlist)
			require.NoError(t, err)
			require.NotNil(t, result)

			tc.verify(t, result)
		})
	}
}
