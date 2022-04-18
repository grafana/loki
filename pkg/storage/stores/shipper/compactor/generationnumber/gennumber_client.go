package generationnumber

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/util/log"
)

const (
	orgHeaderKey    = "X-Scope-OrgID"
	cacheGenNumPath = "/loki/api/v1/generation_numbers"
)

type CacheGenClient interface {
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)
	Name() string
}

type genNumberClient struct {
	addr       string
	httpClient doer
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

func NewGenNumberClient(addr string, c doer) CacheGenClient {
	return &genNumberClient{
		addr:       addr,
		httpClient: c,
	}
}

func (c *genNumberClient) Name() string {
	return "gen_number_client"
}

func (c *genNumberClient) GetCacheGenerationNumber(_ context.Context, userID string) (string, error) {
	u, err := url.Parse(c.addr)
	if err != nil {
		level.Error(log.Logger).Log("msg", "error parsing url", "err", err)
		return "", err
	}

	u.Path = cacheGenNumPath
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		level.Error(log.Logger).Log("msg", "error getting cache gen numbers from the store", "err", err)
		return "", err
	}

	req.Header.Set(orgHeaderKey, userID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		level.Error(log.Logger).Log("msg", "error getting cache gen numbers from the store", "err", err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		level.Error(log.Logger).Log("msg", "error getting cache gen numbers from the store", "err", err)
		return "", err
	}

	var genNumber string
	if err := json.NewDecoder(resp.Body).Decode(&genNumber); err != nil {
		level.Error(log.Logger).Log("msg", "error marshalling response", "err", err)
		return "", err
	}

	return genNumber, err
}
