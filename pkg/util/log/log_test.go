package log

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-kit/log"
	dslog "github.com/grafana/dskit/log"
	"github.com/stretchr/testify/assert"
)

func TestLevelHandler(t *testing.T) {
	var lvl dslog.Level
	err := lvl.Set("info")
	assert.NoError(t, err)
	plogger = &prometheusLogger{
		baseLogger: log.NewLogfmtLogger(io.Discard),
	}

	testCases := []struct {
		testName           string
		targetLogLevel     string
		expectedResponse   string
		expectedLogLevel   string
		expectedStatusCode int
	}{
		{"GetLogLevel", "", `{"message":"Current log level is info"}`, "info", 200},
		{"PostLogLevelInvalid", "invalid", `{"message":"unrecognized log level \"invalid\"", "status":"failed"}`, "info", 400},
		{"PostLogLevelEmpty", "", `{"message":"unrecognized log level \"\"", "status":"failed"}`, "info", 400},
		{"PostLogLevelDebug", "debug", `{"status": "success", "message":"Log level set to debug"}`, "debug", 200},
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(t *testing.T) {
			var (
				req *http.Request
				err error
			)

			if strings.HasPrefix(testCase.testName, "Get") {
				req, err = http.NewRequest("GET", "/", nil)
			} else if strings.HasPrefix(testCase.testName, "Post") {
				form := url.Values{"log_level": {testCase.targetLogLevel}}
				req, err = http.NewRequest("POST", "/", strings.NewReader(form.Encode()))
				req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
			}
			assert.NoError(t, err)

			rr := httptest.NewRecorder()
			handler := LevelHandler(&lvl)
			handler.ServeHTTP(rr, req)

			assert.JSONEq(t, testCase.expectedResponse, rr.Body.String())
			assert.Equal(t, testCase.expectedStatusCode, rr.Code)
			assert.Equal(t, testCase.expectedLogLevel, lvl.String())
		})
	}
}
