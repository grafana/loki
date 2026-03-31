package discover

import (
	"time"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// KeywordAPI is the subset of the Loki client interface required by
// RunKeywordProbing. Abstracting to this minimal interface makes unit testing
// simpler.
type KeywordAPI interface {
	// QueryRange issues a query_range request to the Loki HTTP API. Used to
	// detect keyword presence in a stream via line filter queries.
	QueryRange(
		queryStr string,
		limit int,
		start time.Time,
		end time.Time,
		direction logproto.Direction,
		step time.Duration,
		interval time.Duration,
		quiet bool,
	) (*loghttp.QueryResponse, error)
}

// KeywordResult holds the output of a completed RunKeywordProbing invocation.
type KeywordResult struct {
	// ByKeyword maps each keyword to the sorted list of selectors whose log
	// lines contain that keyword.
	ByKeyword map[string][]string

	// Warnings accumulates non-fatal issues encountered during probing, such
	// as per-probe API errors that caused a probe to be skipped.
	Warnings []string

	// TotalProbed is the number of keyword probes that completed successfully.
	TotalProbed int

	// TotalSkipped is the number of pairs skipped due to API error.
	TotalSkipped int
}
