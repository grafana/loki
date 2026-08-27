package loki

// DrilldownConfigResponse represents the structure for the drilldown config endpoint
// This endpoint returns the filtered tenant limits in a JSON-optimized format
type DrilldownConfigResponse struct {
	Limits                 map[string]any `json:"limits"`
	PatternIngesterEnabled bool           `json:"pattern_ingester_enabled"`
	Version                string         `json:"version"`
	// Mode is "active" (the tenant's currently effective limits, override or fallback default — the
	// default reporting) or "defaults" when ?mode=defaults was requested and honored. An older Loki
	// without mode=defaults support omits this field entirely rather than misreporting it.
	Mode string `json:"mode"`
}
