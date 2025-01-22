package util_test

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestRenderHTTPResponse(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name                string
		headers             map[string]string
		tmpl                string
		expectedOutput      string
		expectedContentType string
		value               testStruct
	}{
		{
			name: "Test Renders json",
			headers: map[string]string{
				"Accept": "application/json",
			},
			tmpl:                "<html></html>",
			expectedOutput:      `{"name":"testName","value":42}`,
			expectedContentType: "application/json",
			value: testStruct{
				Name:  "testName",
				Value: 42,
			},
		},
		{
			name:                "Test Renders html",
			headers:             map[string]string{},
			tmpl:                "<html>{{ .Name }}</html>",
			expectedOutput:      "<html>testName</html>",
			expectedContentType: "text/html; charset=utf-8",
			value: testStruct{
				Name:  "testName",
				Value: 42,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := template.Must(template.New("webpage").Parse(tt.tmpl))
			writer := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/", nil)

			for k, v := range tt.headers {
				request.Header.Add(k, v)
			}

			util.RenderHTTPResponse(writer, tt.value, tmpl, request)

			assert.Equal(t, tt.expectedContentType, writer.Header().Get("Content-Type"))
			assert.Equal(t, 200, writer.Code)
			assert.Equal(t, tt.expectedOutput, writer.Body.String())
		})
	}
}

func TestWriteTextResponse(t *testing.T) {
	w := httptest.NewRecorder()

	util.WriteTextResponse(w, "hello world")

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "hello world", w.Body.String())
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
}

func TestStreamWriteYAMLResponse(t *testing.T) {
	type testStruct struct {
		Name  string `yaml:"name"`
		Value int    `yaml:"value"`
	}
	tt := struct {
		name                string
		headers             map[string]string
		expectedOutput      string
		expectedContentType string
		value               map[string]*testStruct
	}{
		name: "Test Stream Render YAML",
		headers: map[string]string{
			"Content-Type": "application/yaml",
		},
		expectedContentType: "application/yaml",
		value:               make(map[string]*testStruct),
	}

	// Generate some data to serialize.
	for i := 0; i < rand.Intn(100)+1; i++ {
		ts := testStruct{
			Name:  "testName" + strconv.Itoa(i),
			Value: i,
		}
		tt.value[ts.Name] = &ts
	}
	d, err := yaml.Marshal(tt.value)
	require.NoError(t, err)
	tt.expectedOutput = string(d)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	iter := make(chan interface{})
	go func() {
		util.StreamWriteYAMLResponse(w, iter, util_log.Logger)
		close(done)
	}()
	for k, v := range tt.value {
		iter <- map[string]*testStruct{k: v}
	}
	close(iter)
	<-done
	assert.Equal(t, tt.expectedContentType, w.Header().Get("Content-Type"))
	assert.Equal(t, 200, w.Code)
	assert.YAMLEq(t, tt.expectedOutput, w.Body.String())
}

func TestParseProtoReader(t *testing.T) {
	// 47 bytes compressed and 53 uncompressed
	req := &logproto.PreallocWriteRequest{
		WriteRequest: logproto.WriteRequest{
			Timeseries: []logproto.PreallocTimeseries{
				{
					TimeSeries: &logproto.TimeSeries{
						Labels: []logproto.LabelAdapter{
							{Name: "foo", Value: "bar"},
						},
						Samples: []logproto.LegacySample{
							{Value: 10, TimestampMs: 1},
							{Value: 20, TimestampMs: 2},
							{Value: 30, TimestampMs: 3},
						},
					},
				},
			},
		},
	}

	for _, tt := range []struct {
		name           string
		compression    util.CompressionType
		maxSize        int
		expectErr      bool
		useBytesBuffer bool
	}{
		{"rawSnappy", util.RawSnappy, 53, false, false},
		{"noCompression", util.NoCompression, 53, false, false},
		{"too big rawSnappy", util.RawSnappy, 10, true, false},
		{"too big decoded rawSnappy", util.RawSnappy, 50, true, false},
		{"too big noCompression", util.NoCompression, 10, true, false},

		{"bytesbuffer rawSnappy", util.RawSnappy, 53, false, true},
		{"bytesbuffer noCompression", util.NoCompression, 53, false, true},
		{"bytesbuffer too big rawSnappy", util.RawSnappy, 10, true, true},
		{"bytesbuffer too big decoded rawSnappy", util.RawSnappy, 50, true, true},
		{"bytesbuffer too big noCompression", util.NoCompression, 10, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			assert.Nil(t, util.SerializeProtoResponse(w, req, tt.compression))
			var fromWire logproto.PreallocWriteRequest

			reader := w.Result().Body
			if tt.useBytesBuffer {
				buf := bytes.Buffer{}
				_, err := buf.ReadFrom(reader)
				assert.Nil(t, err)
				reader = bytesBuffered{Buffer: &buf}
			}

			err := util.ParseProtoReader(context.Background(), reader, 0, tt.maxSize, &fromWire, tt.compression)
			if tt.expectErr {
				assert.NotNil(t, err)
				return
			}
			assert.Nil(t, err)
			assert.Equal(t, req, &fromWire)
		})
	}
}

type bytesBuffered struct {
	*bytes.Buffer
}

func (b bytesBuffered) Close() error {
	return nil
}

func (b bytesBuffered) BytesBuffer() *bytes.Buffer {
	return b.Buffer
}

func TestIsRequestBodyTooLargeRegression(t *testing.T) {
	_, err := io.ReadAll(http.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(bytes.NewReader([]byte{1, 2, 3, 4})), 1))
	assert.True(t, util.IsRequestBodyTooLarge(err))
}

func TestErrorTypeFromHTTPStatus(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		expectedResult string
	}{
		{
			name:           "rate limited error",
			status:         429,
			expectedResult: util.HTTPRateLimited,
		},
		{
			name:           "server error - 500",
			status:         500,
			expectedResult: util.HTTPServerError,
		},
		{
			name:           "server error - 503",
			status:         503,
			expectedResult: util.HTTPServerError,
		},
		{
			name:           "client error - 400",
			status:         400,
			expectedResult: util.HTTPClientError,
		},
		{
			name:           "client error - 404",
			status:         404,
			expectedResult: util.HTTPClientError,
		},
		{
			name:           "success status should return unknown - 200",
			status:         200,
			expectedResult: util.HTTPErrorUnknown,
		},
		{
			name:           "success status should return unknown - 201",
			status:         201,
			expectedResult: util.HTTPErrorUnknown,
		},
		{
			name:           "invalid status should return unknown - 600",
			status:         600,
			expectedResult: util.HTTPClientError,
		},
		{
			name:           "invalid status should return unknown - -1",
			status:         -1,
			expectedResult: util.HTTPClientError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.ErrorTypeFromHTTPStatus(tt.status)
			assert.Equal(t, tt.expectedResult, result, "ErrorTypeFromHTTPStatus(%d) = %s; want %s",
				tt.status, result, tt.expectedResult)
		})
	}
}

func TestIsError(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		expectedResult bool
	}{
		{
			name:           "200 OK",
			status:         200,
			expectedResult: false,
		},
		{
			name:           "201 Created",
			status:         201,
			expectedResult: false,
		},
		{
			name:           "400 Bad Request",
			status:         400,
			expectedResult: true,
		},
		{
			name:           "404 Not Found",
			status:         404,
			expectedResult: true,
		},
		{
			name:           "429 Too Many Requests",
			status:         429,
			expectedResult: true,
		},
		{
			name:           "500 Internal Server Error",
			status:         500,
			expectedResult: true,
		},
		{
			name:           "503 Service Unavailable",
			status:         503,
			expectedResult: true,
		},
		{
			name:           "600 Unknown",
			status:         600,
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.IsError(tt.status)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}
