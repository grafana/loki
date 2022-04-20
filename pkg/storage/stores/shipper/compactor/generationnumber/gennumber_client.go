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
	cacheGenNumPath = "/loki/api/v1/cache/generation_numbers"
)

type CacheGenClient interface {
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)
	Name() string
}

type genNumberClient struct {
	url        string
	httpClient doer
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

func NewGenNumberClient(addr string, c doer) (CacheGenClient, error) {
	u, err := url.Parse(addr)
	if err != nil {
		level.Error(log.Logger).Log("msg", "error parsing url", "err", err)
		return nil, err
	}
	u.Path = cacheGenNumPath

	return &genNumberClient{
		url:        u.String(),
		httpClient: c,
	}, nil
}

func (c *genNumberClient) Name() string {
	return "gen_number_client"
}

func (c *genNumberClient) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
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
