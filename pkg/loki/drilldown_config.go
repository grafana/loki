package loki

// DrilldownConfigResponse represents the structure for the drilldown config endpoint
// This endpoint returns the filtered tenant limits in a JSON-optimized format
type DrilldownConfigResponse struct {
	Limits                 map[string]any `json:"limits"`
	PatternIngesterEnabled bool           `json:"pattern_ingester_enabled"`
	Version                string         `json:"version"`
}
