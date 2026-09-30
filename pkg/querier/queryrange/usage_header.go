package queryrange

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// queryBytesProcessedHeader exposes scan usage without requiring clients or
// gateways to buffer, decompress, or parse the response body.
const queryBytesProcessedHeader = "X-Loki-Query-Bytes-Processed"

// setQueryBytesProcessedHeader reads the final in-memory statistics. In particular,
// do not forward a sub-response header for regular queries: its value may predate
// splitting, sharding, or cache aggregation.
func setQueryBytesProcessedHeader(headers http.Header, response queryrangebase.Response) {
	headers.Del(queryBytesProcessedHeader)
	var n int64
	switch r := response.(type) {
	case *LokiResponse:
		n = r.Statistics.Summary.TotalBytesProcessed
	case *LokiPromResponse:
		n = r.Statistics.Summary.TotalBytesProcessed
	case *DetectedFieldsResponse:
		var ok bool
		n, ok = queryBytesProcessed(r.GetHeaders())
		if !ok {
			return
		}
	default:
		return
	}
	if n >= 0 {
		headers.Set(queryBytesProcessedHeader, strconv.FormatInt(n, 10))
	}
}

func withQueryBytesProcessed(headers []queryrangebase.PrometheusResponseHeader, value string) []queryrangebase.PrometheusResponseHeader {
	out := make([]queryrangebase.PrometheusResponseHeader, 0, len(headers)+1)
	for _, h := range headers {
		if !strings.EqualFold(h.Name, queryBytesProcessedHeader) {
			out = append(out, h)
		}
	}
	if value != "" {
		out = append(out, queryrangebase.PrometheusResponseHeader{Name: queryBytesProcessedHeader, Values: []string{value}})
	}
	return out
}

func queryBytesProcessed(headers []*queryrangebase.PrometheusResponseHeader) (int64, bool) {
	for _, h := range headers {
		if strings.EqualFold(h.Name, queryBytesProcessedHeader) && len(h.Values) == 1 {
			n, err := strconv.ParseInt(h.Values[0], 10, 64)
			return n, err == nil && n >= 0
		}
	}
	return 0, false
}

// Omit aggregate usage if any response lacks it (for example during a rolling
// upgrade). Reporting only the first split would look like a complete total.
func mergeQueryBytesProcessed(responses []queryrangebase.Response) []queryrangebase.PrometheusResponseHeader {
	var total int64
	for _, response := range responses {
		n, ok := queryBytesProcessed(response.GetHeaders())
		if !ok || n > math.MaxInt64-total {
			return withQueryBytesProcessed(responses[0].(*DetectedFieldsResponse).Headers, "")
		}
		total += n
	}
	return withQueryBytesProcessed(responses[0].(*DetectedFieldsResponse).Headers, strconv.FormatInt(total, 10))
}
