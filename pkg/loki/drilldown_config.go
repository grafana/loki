package loki

// DrilldownConfigResponse represents the structure for the drilldown config endpoint
// This endpoint returns the filtered tenant limits in a JSON-optimized format
type DrilldownConfigResponse struct {
	Limits                 map[string]any `json:"limits"`
	PatternIngesterEnabled bool           `json:"pattern_ingester_enabled"`
	Version                string         `json:"version"`
	// Mode reflects which source Limits actually came from ("tenant" or "defaults"), regardless of
	// whether mode=defaults was requested — a caller can't trust the request alone, since an older Loki
	// without mode=defaults support ignores the param and returns the tenant's current value instead.
	Mode string `json:"mode"`
}
