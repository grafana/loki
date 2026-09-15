package loki

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/flagext"

	"github.com/grafana/loki/v3/pkg/pattern"
	"github.com/grafana/loki/v3/pkg/validation"
)

type diffConfigMock struct {
	MyInt          int          `yaml:"my_int"`
	MyFloat        float64      `yaml:"my_float"`
	MySlice        []string     `yaml:"my_slice"`
	IgnoredField   func() error `yaml:"-"`
	MyNestedStruct struct {
		MyString      string   `yaml:"my_string"`
		MyBool        bool     `yaml:"my_bool"`
		MyEmptyStruct struct{} `yaml:"my_empty_struct"`
	} `yaml:"my_nested_struct"`
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
				"    - value1\n" +
				"    - value2\n" +
				"    - value3\n",
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
				"    my_string: string2\n",
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
				"    my_bool: true\n",
		},
		{
			name: "test invalid input",
			actualConfig: func() any {
				c := "x"
				return &c
			},
			expectedStatusCode: 500,
			expectedBody:       "yaml: construct errors: line 1: cannot construct !!str `x` into map[string]interface {}\n",
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

func TestConfigQueryHandler(t *testing.T) {
	cfg := newDefaultDiffConfigMock()

	tooManyPaths := make(url.Values)
	for i := 0; i < maxConfigQueryPaths+1; i++ {
		tooManyPaths.Add("q", "my_int")
	}

	for _, tc := range []struct {
		name                string
		query               string
		acceptHeader        string
		expectedStatusCode  int
		expectedContentType string
		expectedHeader      []string
		expectedBody        string
	}{
		{
			name:               "single top-level path",
			query:              "q=my_int",
			expectedStatusCode: 200,
			expectedHeader:     []string{"my_int"},
			expectedBody:       "my_int: 666\n",
		},
		{
			name:               "nested path",
			query:              "q=my_nested_struct.my_string",
			expectedStatusCode: 200,
			expectedHeader:     []string{"my_nested_struct.my_string"},
			expectedBody:       "my_nested_struct:\n    my_string: string1\n",
		},
		{
			name:               "multiple paths in one request",
			query:              "q=my_int&q=my_float",
			expectedStatusCode: 200,
			expectedHeader:     []string{"my_int", "my_float"},
			expectedBody:       "my_float: 6.66\nmy_int: 666\n",
		},
		{
			name:               "paths sharing a parent are merged, not overwritten",
			query:              "q=my_nested_struct.my_string&q=my_nested_struct.my_bool",
			expectedStatusCode: 200,
			expectedHeader:     []string{"my_nested_struct.my_string", "my_nested_struct.my_bool"},
			expectedBody:       "my_nested_struct:\n    my_bool: false\n    my_string: string1\n",
		},
		{
			name:               "malformed query string returns 400, not the unfiltered config",
			query:              "q=my_int;unexpected",
			expectedStatusCode: 400,
		},
		{
			name:               "unknown path returns 400",
			query:              "q=does.not.exist",
			expectedStatusCode: 400,
			// The header still reflects what was recognized/attempted, even though it didn't resolve.
			expectedHeader: []string{"does.not.exist"},
		},
		{
			name:               "no q param leaves the header unset and behaves as before",
			query:              "",
			expectedStatusCode: 200,
		},
		{
			name:               "too many q parameters returns 400",
			query:              tooManyPaths.Encode(),
			expectedStatusCode: 400,
		},
		{
			name:               "q parameter too long returns 400",
			query:              "q=" + strings.Repeat("a", maxConfigQueryPathLength+1),
			expectedStatusCode: 400,
		},
		{
			name:               "malformed q parameter/empty",
			query:              url.Values{"q": {""}}.Encode(),
			expectedStatusCode: 400,
		},
		{
			name:               "malformed q parameter/trailing dot",
			query:              url.Values{"q": {"my_int."}}.Encode(),
			expectedStatusCode: 400,
		},
		{
			name:               "malformed q parameter/leading dot",
			query:              url.Values{"q": {".my_int"}}.Encode(),
			expectedStatusCode: 400,
		},
		{
			name:               "malformed q parameter/double dot",
			query:              url.Values{"q": {"my_int..my_float"}}.Encode(),
			expectedStatusCode: 400,
		},
		{
			name:               "malformed q parameter/control characters",
			query:              url.Values{"q": {"my_int\r\nX-Injected: evil"}}.Encode(),
			expectedStatusCode: 400,
		},
		{
			name:               "malformed q parameter/space",
			query:              url.Values{"q": {"my int"}}.Encode(),
			expectedStatusCode: 400,
		},
		{
			name:                "base config returns JSON when Accept asks for it",
			acceptHeader:        "application/json",
			expectedStatusCode:  200,
			expectedContentType: "application/json",
			expectedBody:        `{"my_float":6.66,"my_int":666,"my_nested_struct":{"my_bool":false,"my_empty_struct":{},"my_string":"string1"},"my_slice":["value1","value2"]}` + "\n",
		},
		{
			name:                "q-scoped response returns JSON when Accept asks for it",
			query:               "q=my_nested_struct.my_string&q=my_int",
			acceptHeader:        "application/json",
			expectedStatusCode:  200,
			expectedContentType: "application/json",
			expectedHeader:      []string{"my_nested_struct.my_string", "my_int"},
			expectedBody:        `{"my_int":666,"my_nested_struct":{"my_string":"string1"}}` + "\n",
		},
		{
			name:                "a multi-value Accept header still negotiates JSON",
			acceptHeader:        "text/html, application/json;q=0.9, */*;q=0.8",
			expectedStatusCode:  200,
			expectedContentType: "application/json",
		},
		{
			name:                "q=0 explicitly excludes JSON, falling back to YAML",
			acceptHeader:        "application/json;q=0",
			expectedStatusCode:  200,
			expectedContentType: "text/plain; charset=utf-8",
		},
		{
			name:                "a lookalike media type doesn't false-positive as JSON",
			acceptHeader:        "application/json-seq",
			expectedStatusCode:  200,
			expectedContentType: "text/plain; charset=utf-8",
		},
		{
			name:                "an unparseable q value falls back to YAML rather than defaulting to accepted",
			acceptHeader:        "application/json;q=bogus",
			expectedStatusCode:  200,
			expectedContentType: "text/plain; charset=utf-8",
		},
		{
			name:                "a q value outside 0-1 falls back to YAML rather than defaulting to accepted",
			acceptHeader:        "application/json;q=2",
			expectedStatusCode:  200,
			expectedContentType: "text/plain; charset=utf-8",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://test.com/config?"+tc.query, nil)
			if tc.acceptHeader != "" {
				req.Header.Set("Accept", tc.acceptHeader)
			}
			w := httptest.NewRecorder()

			configHandler(cfg, cfg)(w, req)
			resp := w.Result()
			assert.Equal(t, tc.expectedStatusCode, resp.StatusCode)
			assert.Equal(t, tc.expectedHeader, resp.Header.Values(ConfigQueryHandledHeader))

			if tc.expectedContentType != "" {
				assert.Equal(t, tc.expectedContentType, resp.Header.Get("Content-Type"))
			}

			// The representation always depends on Accept once the request reaches a response,
			// regardless of which one was chosen.
			if tc.expectedStatusCode == 200 {
				assert.Equal(t, "Accept", resp.Header.Get("Vary"))
			}

			if tc.expectedBody != "" {
				body, err := io.ReadAll(resp.Body)
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedBody, string(body))
			}
		})
	}
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

	handler := loki.tenantLimitsHandler(true)

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

			handler := loki.tenantLimitsHandler(true)

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

func TestDrilldownConfig(t *testing.T) {
	testCases := []struct {
		name                string
		limits              *validation.Limits
		allowlist           []string
		patternEnabled      bool
		expectedStatus      int
		expectedContentType string
		verifyResponse      func(t *testing.T, response DrilldownConfigResponse)
	}{
		{
			name: "response structure with all fields and pattern enabled",
			limits: &validation.Limits{
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
				VolumeEnabled:           true,
				VolumeMaxSeries:         2000,
				MaxQueryBytesRead:       flagext.ByteSize(1024 * 1024),
			},
			allowlist:           []string{}, // Empty allowlist = all fields
			patternEnabled:      true,
			expectedStatus:      200,
			expectedContentType: "application/json",
			verifyResponse: func(t *testing.T, response DrilldownConfigResponse) {
				// Response should have a limits field containing the filtered limits
				require.NotNil(t, response.Limits)

				// Check a few key fields to verify the limits were included
				assert.Equal(t, float64(10.5), response.Limits["ingestion_rate_mb"])
				assert.Equal(t, float64(15), response.Limits["ingestion_burst_size_mb"])
				assert.Equal(t, float64(100), response.Limits["max_label_name_length"])
				assert.Equal(t, float64(1000), response.Limits["max_query_series"])
				assert.Equal(t, float64(500), response.Limits["max_streams_per_user"])
				assert.Equal(t, float64(5000), response.Limits["max_entries_limit_per_query"])
				assert.Equal(t, "1MB", response.Limits["max_query_bytes_read"]) // ByteSize serializes as string
				assert.Equal(t, true, response.Limits["volume_enabled"])
				assert.Equal(t, float64(2000), response.Limits["volume_max_series"])

				// Check pattern ingester enabled field
				assert.Equal(t, true, response.PatternIngesterEnabled)

				// Check version field is present (we don't check the exact value as it may vary)
				assert.NotEmpty(t, response.Version)
			},
		},
		{
			name: "pattern ingester disabled",
			limits: &validation.Limits{
				IngestionRateMB: 10.5,
				MaxQuerySeries:  1000,
			},
			allowlist:           []string{},
			patternEnabled:      false,
			expectedStatus:      200,
			expectedContentType: "application/json",
			verifyResponse: func(t *testing.T, response DrilldownConfigResponse) {
				// Pattern ingester should be disabled
				assert.Equal(t, false, response.PatternIngesterEnabled)

				// Version should still be present
				assert.NotEmpty(t, response.Version)
			},
		},
		{
			name: "with allowlist filter",
			limits: &validation.Limits{
				IngestionRateMB:        10.5,
				MaxQuerySeries:         1000,
				MaxLocalStreamsPerUser: 500,
				MaxLabelNameLength:     100,
			},
			allowlist:           []string{"ingestion_rate_mb", "max_query_series"},
			patternEnabled:      false,
			expectedStatus:      200,
			expectedContentType: "application/json",
			verifyResponse: func(t *testing.T, response DrilldownConfigResponse) {
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
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockTenantLimits := &mockTenantLimits{limits: tc.limits}

			loki := &Loki{
				TenantLimits: mockTenantLimits,
				Cfg: Config{
					TenantLimitsAllowPublish: tc.allowlist,
					Pattern: pattern.Config{
						Enabled: tc.patternEnabled,
					},
				},
			}

			handler := loki.tenantLimitsHandler(true)

			req := httptest.NewRequest("GET", "/loki/api/v1/config", nil)
			req.Header.Set("X-Scope-OrgID", "test-tenant")

			w := httptest.NewRecorder()
			handler(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			// Verify status code and content type
			require.Equal(t, tc.expectedStatus, resp.StatusCode)
			assert.Equal(t, tc.expectedContentType, resp.Header.Get("Content-Type"))

			// Parse response
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var response DrilldownConfigResponse
			err = json.Unmarshal(body, &response)
			require.NoError(t, err, "Response should be valid JSON")

			// Run test-specific verifications
			tc.verifyResponse(t, response)
		})
	}
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
