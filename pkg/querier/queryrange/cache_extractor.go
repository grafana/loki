package queryrange

import (
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
)

// SeriesExtractor implements Extractor interface
type SeriesExtractor struct{}

// Extract just returns response. We don't need any custom implementation for "series" requests
func (SeriesExtractor) Extract(start, end int64, from queryrange.Response) queryrange.Response {
	return from
}

// ResponseWithoutHeaders just returns response.
// We don't need any custom implementation for "series" requests
func (SeriesExtractor) ResponseWithoutHeaders(resp queryrange.Response) queryrange.Response {
	return resp
}
