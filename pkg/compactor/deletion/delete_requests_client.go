package deletion

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/util/log"
)

type CompactorClient interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error)
	GetCacheGenerationNumber(ctx context.Context, userID string) (string, error)
	Name() string
	Stop()
}

type DeleteRequestsClient interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error)
	Stop()
}

type deleteRequestsClient struct {
	compactorClient CompactorClient
	mu              sync.RWMutex

	cache         map[string][]DeleteRequest
	cacheDuration time.Duration

	metrics    *DeleteRequestClientMetrics
	clientType string

	stopChan chan struct{}
}

type DeleteRequestsStoreOption func(c *deleteRequestsClient)

func WithRequestClientCacheDuration(d time.Duration) DeleteRequestsStoreOption {
	return func(c *deleteRequestsClient) {
		c.cacheDuration = d
	}
}

func NewDeleteRequestsClient(compactorClient CompactorClient, deleteClientMetrics *DeleteRequestClientMetrics, clientType string, opts ...DeleteRequestsStoreOption) (DeleteRequestsClient, error) {
	client := &deleteRequestsClient{
		compactorClient: compactorClient,
		cacheDuration:   5 * time.Minute,
		cache:           make(map[string][]DeleteRequest),
		clientType:      clientType,
		metrics:         deleteClientMetrics,
		stopChan:        make(chan struct{}),
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

	c.metrics.deleteRequestsLookupsTotal.Inc()
	requests, err := c.compactorClient.GetAllDeleteRequestsForUser(ctx, userID)
	if err != nil {
		c.metrics.deleteRequestsLookupsFailedTotal.Inc()
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
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if err := c.updateCache(); err != nil {
				level.Error(log.Logger).Log("msg", "error reloading cached delete requests", "err", err)
			}
		case <-c.stopChan:
			return
		}
	}
}

func (c *deleteRequestsClient) updateCache() error {
	userIDs := c.currentUserIDs()

	newCache := make(map[string][]DeleteRequest)
	for _, userID := range userIDs {
		deleteReq, err := c.compactorClient.GetAllDeleteRequestsForUser(context.Background(), userID)
		if err != nil {
			return err
		}
		newCache[userID] = deleteReq
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = newCache

	return nil
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

func NewNoOpDeleteRequestsClient() DeleteRequestsClient {
	return &noOpDeleteRequestsClient{}
}

type noOpDeleteRequestsClient struct{}

func (n noOpDeleteRequestsClient) GetAllDeleteRequestsForUser(_ context.Context, _ string) ([]DeleteRequest, error) {
	return nil, nil
}

func (n noOpDeleteRequestsClient) Stop() {}
