package loki

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/util/build"
)

func TestVersionHandler(t *testing.T) {
	build.Version = "0.0.1"
	build.Branch = "main"
	build.Revision = "foobar"
	build.BuildDate = "yesterday"
	build.BuildUser = "Turing"

	req := httptest.NewRequest("GET", "http://test.com/config?mode=diff", nil)
	w := httptest.NewRecorder()

	h := versionHandler()
	h(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	expected := `{
		"version":"0.0.1",
		"branch":"main",
		"build_date":"yesterday",
		"build_user":"Turing",
		"revision":"foobar"
	}`
	body, err := ioutil.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.JSONEq(t, expected, string(body))
}
