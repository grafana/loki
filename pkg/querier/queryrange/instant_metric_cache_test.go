package queryrange

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	base "github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/pkg/util/constants"
	"github.com/stretchr/testify/require"
)

var (
	instantTestTime = time.Now()
	defaultRange    = "1h"
	queryString     = fmt.Sprintf(`sum(rate{foo="bar"}[%s])`, defaultRange)
	params          = url.Values{
		"query": []string{queryString},
		"time":  []string{fmt.Sprintf("%d", instantTestTime.UTC().Unix())},
	}
	queryURLEncoded = fmt.Sprintf("/api/v1/query?%s", params.Encode())

	parsedInstantQueryRequest = &LokiInstantRequest{
		Path:   "/api/v1/query",
		TimeTs: testTime,
		Query:  queryString,
	}
)

func TestCacheMiddleware(t *testing.T) {
	cfg := base.ResultsCacheConfig{
		Config: resultscache.Config{
			CacheConfig: cache.Config{
				Cache: cache.NewMockCache(),
			},
		},
	}

	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
	require.NoError(t, err)

	m, err := NewInstantMetricCacheMiddleware(
		log.NewNopLogger(),
		c,
	)
}
