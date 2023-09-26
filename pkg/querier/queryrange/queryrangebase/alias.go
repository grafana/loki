package queryrangebase

import "github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"

// Helpful aliases for refactoring circular imports

type CachingOptions = definitions.CachingOptions
type PrometheusResponseHeader = definitions.PrometheusResponseHeader
type PrometheusRequestHeader = definitions.PrometheusRequestHeader
type Codec = definitions.Codec
type Merger = definitions.Merger
type Request = definitions.Request
type Response = definitions.Response
