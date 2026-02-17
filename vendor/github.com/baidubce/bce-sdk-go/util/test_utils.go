package util

import (
	"bytes"
	"io"
	"net/http"
	"reflect"
	"time"
)

func Equal(expected, actual interface{}) bool {
	if expected == nil && actual == nil {
		return true
	}
	if expected != nil && actual == nil {
		return reflect.ValueOf(expected).IsNil()
	}
	if expected == nil && actual != nil {
		return reflect.ValueOf(actual).IsNil()
	}
	actualType := reflect.TypeOf(actual)
	expectedValue := reflect.ValueOf(expected)
	if expectedValue.IsValid() && expectedValue.Type().ConvertibleTo(actualType) {
		return reflect.DeepEqual(expectedValue.Convert(actualType).Interface(), actual)
	}
	return reflect.DeepEqual(expected, actual)
}

type MockRoundTripperOption func(*MockRoundTripper)

type MockRoundTripper struct {
	Err         error
	StatusCode  int
	StatusMsg   string
	RespBody    []string
	RequestTime *time.Duration
	Headers     map[string]string
	RespCount   int
}

var (
	RoundTripperOpts403 = []MockRoundTripperOption{SetStatusCode(http.StatusForbidden), SetStatusMsg("403 Forbidden")}
	RoundTripperOpts404 = []MockRoundTripperOption{SetStatusCode(http.StatusNotFound), SetStatusMsg("404 NOT Found")}
	RoundTripperOpts408 = []MockRoundTripperOption{SetStatusCode(http.StatusRequestTimeout), SetStatusMsg(http.StatusText(http.StatusRequestTimeout))}
	RoundTripperOpts500 = []MockRoundTripperOption{SetStatusCode(http.StatusInternalServerError), SetStatusMsg(http.StatusText(http.StatusInternalServerError))}
)

func SetHTTPClientDoError(err error) MockRoundTripperOption {
	return func(m *MockRoundTripper) { m.Err = err }
}

func SetStatusCode(statusCode int) MockRoundTripperOption {
	return func(m *MockRoundTripper) { m.StatusCode = statusCode }
}

func SetStatusMsg(statusMsg string) MockRoundTripperOption {
	return func(m *MockRoundTripper) { m.StatusMsg = statusMsg }
}

func SetRespBody(respBody string) MockRoundTripperOption {
	return func(m *MockRoundTripper) { m.RespBody = []string{respBody} }
}

func AppendRespBody(respBody []string) MockRoundTripperOption {
	return func(m *MockRoundTripper) { m.RespBody = append(m.RespBody, respBody...) }
}

func SetRequestTime(value time.Duration) MockRoundTripperOption {
	return func(m *MockRoundTripper) { m.RequestTime = &value }
}

func AddHeaders(kv map[string]string) MockRoundTripperOption {
	return func(m *MockRoundTripper) {
		if m.Headers == nil {
			m.Headers = make(map[string]string)
		}
		for k, v := range kv {
			m.Headers[k] = v
		}
	}
}

func (m *MockRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if m.Err != nil {
		return nil, m.Err
	}

	if m.RequestTime != nil {
		time.Sleep(*m.RequestTime)
	}

	if request.Body != nil {
		buf := make([]byte, request.ContentLength)
		_, err := request.Body.Read(buf)
		if err != nil {
			return nil, err
		}
	}

	resp := &http.Response{
		StatusCode: m.StatusCode,
		Status:     m.StatusMsg,
		Header:     make(http.Header),
	}
	respIndex := m.RespCount
	if respIndex >= len(m.RespBody) {
		respIndex = len(m.RespBody) - 1
	}
	if respIndex >= 0 {
		resp.Body = io.NopCloser(bytes.NewBufferString(m.RespBody[respIndex]))
	}
	m.RespCount++
	for k, v := range m.Headers {
		resp.Header[http.CanonicalHeaderKey(k)] = append(resp.Header[k], v)
	}
	return resp, nil
}

func NewMockHTTPClient(options ...MockRoundTripperOption) *http.Client {
	mockRoundTripper := &MockRoundTripper{}
	for _, option := range options {
		option(mockRoundTripper)
	}
	mockRoundTripper.RespCount = 0
	return &http.Client{
		Transport: mockRoundTripper,
	}
}
