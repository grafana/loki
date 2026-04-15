package loki

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/util/build"
)

func TestVersionHandler(t *testing.T) {
	build.Version = "0.0.1"
	build.Branch = "main"
	build.Revision = "foobar"
	build.BuildDate = "yesterday"
	build.BuildUser = "Turing"
	build.GoVersion = "42"

	req := httptest.NewRequest("GET", "http://test.com/config?mode=diff", nil)
	w := httptest.NewRecorder()

	h := versionHandler()
	h(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	expected := `{
		"version":"0.0.1",
		"branch":"main",
		"buildDate":"yesterday",
		"buildUser":"Turing",
		"revision":"foobar",
		"goVersion": "42"
	}`
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.JSONEq(t, expected, string(body))
}
