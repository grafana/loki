package queryrangebase

import (
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

// Helpful aliases for refactoring circular imports

type CachingOptions = definitions.CachingOptions
type PrometheusResponseHeader = definitions.PrometheusResponseHeader
type PrometheusRequestHeader = definitions.PrometheusRequestHeader
type Codec = definitions.Codec
type Merger = definitions.Merger
type CacheGenNumberLoader = resultscache.CacheGenNumberLoader

type Request = definitions.Request
type Response = definitions.Response
type Extent = resultscache.Extent
