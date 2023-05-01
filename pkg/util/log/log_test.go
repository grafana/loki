package log

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/weaveworks/common/logging"
)

func TestLevelHandler(t *testing.T) {
	var lvl logging.Level
	err := lvl.Set("info")
	assert.NoError(t, err)
	plogger = &prometheusLogger{
		baseLogger: log.NewLogfmtLogger(io.Discard),
	}

	// Start test http server
	go func() {
		err := http.ListenAndServe(":8080", LevelHandler(&lvl))
		assert.NoError(t, err)
	}()

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
				resp *http.Response
				err  error
			)

			if strings.HasPrefix(testCase.testName, "Get") {
				resp, err = http.Get("http://localhost:8080/")
			} else if strings.HasPrefix(testCase.testName, "Post") {
				resp, err = http.PostForm("http://localhost:8080/", url.Values{"log_level": {testCase.targetLogLevel}})
			}
			assert.NoError(t, err)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, testCase.expectedResponse, string(body))
			assert.Equal(t, testCase.expectedStatusCode, resp.StatusCode)
			assert.Equal(t, testCase.expectedLogLevel, lvl.String())
		})
	}
}
