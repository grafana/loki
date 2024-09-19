package queryrangebase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/dskit/user"

	"github.com/stretchr/testify/assert"
)

func TestCacheGenNumberHeaderSetterMiddleware(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://testing.com", nil)
	w := httptest.NewRecorder()
	loader := &fakeGenNumberLoader{genNumber: "test-header-value"}

	mware := CacheGenNumberHeaderSetterMiddleware(loader).
		Wrap(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	mware.ServeHTTP(w, req)

	assert.Equal(t, w.Header().Get(ResultsCacheGenNumberHeaderName), "test-header-value")
}

type fakeGenNumberLoader struct {
	genNumber string
}

func (l *fakeGenNumberLoader) GetResultsCacheGenNumber(_ []string) string {
	return l.genNumber
}

func (l *fakeGenNumberLoader) Stop() {}
