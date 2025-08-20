package loki

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		actualConfig       func() interface{}
	}{
		{
			name:               "no config parameters overridden",
			expectedStatusCode: 200,
			expectedBody:       "{}\n",
		},
		{
			name: "slice changed",
			actualConfig: func() interface{} {
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
			actualConfig: func() interface{} {
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
			actualConfig: func() interface{} {
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
			actualConfig: func() interface{} {
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

			var actualCfg interface{}
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

func TestConfigHandlerWithActualConfig(t *testing.T) {
	// Test that configHandler can handle the actual Loki Config struct with JSON
	// by converting through YAML first (which handles function fields gracefully)

	defaultCfg := newDefaultConfig()
	actualCfg := newDefaultConfig()

	req := httptest.NewRequest("GET", "http://test.com/config", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	h := configHandler(actualCfg, defaultCfg)
	h(w, req)
	resp := w.Result()

	// Should succeed by converting through YAML first
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var jsonResponse map[string]any
	err = json.Unmarshal(body, &jsonResponse)
	require.NoError(t, err, "Response should be valid JSON")

	// Verify some expected fields are present
	assert.Contains(t, jsonResponse, "auth_enabled")
	assert.Contains(t, jsonResponse, "server")
}

func TestConfigHandlerDiffModeWithJSON(t *testing.T) {
	// Test that diff mode works with JSON (it returns a map, not a struct)

	defaultCfg := newDefaultDiffConfigMock()
	actualCfg := newDefaultDiffConfigMock()
	actualCfg.MyInt = 999

	req := httptest.NewRequest("GET", "http://test.com/config?mode=diff", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	h := configHandler(actualCfg, defaultCfg)
	h(w, req)
	resp := w.Result()

	// Diff mode should work with JSON because it returns a map
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var jsonResponse map[string]any
	err = json.Unmarshal(body, &jsonResponse)
	require.NoError(t, err, "Diff response should be valid JSON")

	// Should only contain the changed field
	assert.Equal(t, float64(999), jsonResponse["my_int"])
	assert.NotContains(t, jsonResponse, "my_float") // unchanged field should not be in diff
}

func TestConfigHandlerContentNegotiation(t *testing.T) {
	// Integration test for content negotiation
	defaultCfg := newDefaultConfig()
	actualCfg := newDefaultConfig()
	handler := configHandler(actualCfg, defaultCfg)

	t.Run("no accept header returns YAML", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/config", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Contains(t, w.Body.String(), "auth_enabled:") // YAML format
	})

	t.Run("accept application/json returns JSON", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/config", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		handler(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var result map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)
		assert.Contains(t, result, "auth_enabled")
	})

	t.Run("accept text/yaml returns YAML", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/config", nil)
		req.Header.Set("Accept", "text/yaml")
		w := httptest.NewRecorder()
		handler(w, req)

		assert.Equal(t, 200, w.Code)
		assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Contains(t, w.Body.String(), "auth_enabled:")
	})
}

func TestUnsupportedAcceptHeaderFallback(t *testing.T) {
	// Test that unsupported Accept headers fall back to YAML

	defaultCfg := newDefaultDiffConfigMock()
	actualCfg := newDefaultDiffConfigMock()
	handler := configHandler(actualCfg, defaultCfg)

	unsupportedTypes := []string{
		"application/xml",
		"text/html",
		"application/pdf",
		"image/png",
		"application/octet-stream",
		"*/*", // wildcard should also default to YAML
	}

	for _, contentType := range unsupportedTypes {
		t.Run(contentType, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/config", nil)
			req.Header.Set("Accept", contentType)
			w := httptest.NewRecorder()

			handler(w, req)

			// Should return YAML (default behavior)
			assert.Equal(t, 200, w.Code)
			assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
			assert.Contains(t, w.Body.String(), "my_int:") // YAML format
		})
	}
}

func TestConfigHandlerJSONResponse(t *testing.T) {
	// Test that configHandler returns JSON when Accept: application/json is sent

	// Setup
	defaultCfg := newDefaultDiffConfigMock()
	actualCfg := newDefaultDiffConfigMock()
	actualCfg.MyInt = 777 // Change something to see it in the response

	req := httptest.NewRequest("GET", "http://test.com/config", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	// Execute
	h := configHandler(actualCfg, defaultCfg)
	h(w, req)
	resp := w.Result()

	// Assert behavior: should return JSON with correct content-type
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Debug: print the actual response if it's not 200
	if resp.StatusCode != 200 {
		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))
	}

	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var jsonResponse map[string]any
	err = json.Unmarshal(body, &jsonResponse)
	require.NoError(t, err, "Response should be valid JSON")

	// Assert behavior: JSON should contain the expected config data
	assert.Equal(t, float64(777), jsonResponse["my_int"], "Config should contain modified value")
	assert.Equal(t, 6.66, jsonResponse["my_float"], "Config should contain default float value")
}

func Test_convertToJSONMap(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		v    any
		want any
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToJSONMap(tt.v)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("convertToJSONMap() = %v, want %v", got, tt.want)
			}
		})
	}
}
