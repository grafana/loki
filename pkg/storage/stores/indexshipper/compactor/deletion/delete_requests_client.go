package deletion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/util/log"
)

const (
	orgHeaderKey  = "X-Scope-OrgID"
	getDeletePath = "/loki/api/v1/delete"
)

type DeleteRequestsClient interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error)
	Stop()
}

type deleteRequestsClient struct {
	url        string
	httpClient httpClient
	mu         sync.RWMutex

	cache         map[string][]DeleteRequest
	cacheDuration time.Duration

	metrics    *DeleteRequestClientMetrics
	clientType string

	stopChan chan struct{}
}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

type DeleteRequestsStoreOption func(c *deleteRequestsClient)

func WithRequestClientCacheDuration(d time.Duration) DeleteRequestsStoreOption {
	return func(c *deleteRequestsClient) {
		c.cacheDuration = d
	}
}

func NewDeleteRequestsClient(addr string, c httpClient, deleteClientMetrics *DeleteRequestClientMetrics, clientType string, opts ...DeleteRequestsStoreOption) (DeleteRequestsClient, error) {
	u, err := url.Parse(addr)
	if err != nil {
		level.Error(log.Logger).Log("msg", "error parsing url", "err", err)
		return nil, err
	}
	u.Path = getDeletePath

	client := &deleteRequestsClient{
		url:           u.String(),
		httpClient:    c,
		cacheDuration: 5 * time.Minute,
		cache:         make(map[string][]DeleteRequest),
		clientType:    clientType,
		metrics:       deleteClientMetrics,
		stopChan:      make(chan struct{}),
	}

	for _, o := range opts {
		o(client)
	}

	go client.updateLoop()
	return client, nil
}

func (c *deleteRequestsClient) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	if cachedRequests, ok := c.getCachedRequests(userID); ok {
		return cachedRequests, nil
	}

	c.metrics.deleteRequestsLookupsTotal.With(prometheus.Labels{"client_type": c.clientType}).Inc()
	requests, err := c.getRequestsFromServer(ctx, userID)
	if err != nil {
		c.metrics.deleteRequestsLookupsFailedTotal.With(prometheus.Labels{"client_type": c.clientType}).Inc()
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[userID] = requests

	return requests, nil
}

func (c *deleteRequestsClient) getCachedRequests(userID string) ([]DeleteRequest, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	res, ok := c.cache[userID]
	return res, ok
}

func (c *deleteRequestsClient) Stop() {
	close(c.stopChan)
}

func (c *deleteRequestsClient) updateLoop() {
	t := time.NewTicker(c.cacheDuration)
	for {
		select {
		case <-t.C:
			c.updateCache()
		case <-c.stopChan:
			return
		}
	}
}

func (c *deleteRequestsClient) updateCache() {
	userIDs := c.currentUserIDs()

	newCache := make(map[string][]DeleteRequest)
	for _, userID := range userIDs {
		deleteReq, err := c.getRequestsFromServer(context.Background(), userID)
		if err != nil {
			level.Error(log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
			continue
		}
		newCache[userID] = deleteReq
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = newCache
}

func (c *deleteRequestsClient) currentUserIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	userIDs := make([]string, 0, len(c.cache))
	for userID := range c.cache {
		userIDs = append(userIDs, userID)
	}

	return userIDs
}

func (c *deleteRequestsClient) getRequestsFromServer(ctx context.Context, userID string) ([]DeleteRequest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		level.Error(log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		return nil, err
	}

	req.Header.Set(orgHeaderKey, userID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		level.Error(log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode/100 != 2 {
		err := fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		level.Error(log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		return nil, err
	}

	var deleteRequests []DeleteRequest
	if err := json.NewDecoder(resp.Body).Decode(&deleteRequests); err != nil {
		level.Error(log.Logger).Log("msg", "error marshalling response", "err", err)
		return nil, err
	}

	return deleteRequests, nil
}
