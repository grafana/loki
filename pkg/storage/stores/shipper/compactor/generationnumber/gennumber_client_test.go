package generationnumber

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetCacheGenNumberForUser(t *testing.T) {
	httpClient := &mockHTTPClient{ret: `"42"`}
	client, err := NewGenNumberClient("http://test-server", httpClient)
	require.Nil(t, err)

	cacheGenNumber, err := client.GetCacheGenerationNumber(context.Background(), "userID")
	require.Nil(t, err)

	require.Equal(t, "42", cacheGenNumber)

	require.Equal(t, "http://test-server/loki/api/v1/cache/generation_numbers", httpClient.req.URL.String())
	require.Equal(t, http.MethodGet, httpClient.req.Method)
	require.Equal(t, "userID", httpClient.req.Header.Get("X-Scope-OrgID"))
}

type mockHTTPClient struct {
	ret string
	req *http.Request
}

func (c *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.req = req

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(c.ret)),
	}, nil
}
