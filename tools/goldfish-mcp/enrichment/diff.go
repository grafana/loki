package enrichment

import "github.com/grafana/loki/v3/pkg/goldfish"

// DifferenceSummary contains boolean comparisons and deltas for response differences
type DifferenceSummary struct {
	StatusCodeMatch   bool  `json:"statusCodeMatch"`
	SizeMatch         bool  `json:"sizeMatch"`
	HashMatch         bool  `json:"hashMatch"`
	SizeDeltaBytes    int64 `json:"sizeDeltaBytes"` // B - A
	CellAStatusCode   int   `json:"cellAStatusCode"`
	CellBStatusCode   int   `json:"cellBStatusCode"`
	CellAResponseSize int64 `json:"cellAResponseSize"`
	CellBResponseSize int64 `json:"cellBResponseSize"`
}

// CalculateDifferenceSummary computes difference summary from a query sample
func CalculateDifferenceSummary(querySample goldfish.QuerySample) DifferenceSummary {
	return DifferenceSummary{
		StatusCodeMatch:   querySample.CellAStatusCode == querySample.CellBStatusCode,
		SizeMatch:         querySample.CellAResponseSize == querySample.CellBResponseSize,
		HashMatch:         querySample.CellAResponseHash == querySample.CellBResponseHash,
		SizeDeltaBytes:    querySample.CellBResponseSize - querySample.CellAResponseSize,
		CellAStatusCode:   querySample.CellAStatusCode,
		CellBStatusCode:   querySample.CellBStatusCode,
		CellAResponseSize: querySample.CellAResponseSize,
		CellBResponseSize: querySample.CellBResponseSize,
	}
}
