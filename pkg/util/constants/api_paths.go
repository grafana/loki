package constants

// Loki API v1 HTTP path constants.
const (
	// Query and query_range
	PathLokiQueryRange = "/loki/api/v1/query_range"
	PathLokiQuery      = "/loki/api/v1/query"
	// Metadata
	PathLokiSeries          = "/loki/api/v1/series"
	PathLokiLabels          = "/loki/api/v1/labels"
	PathLokiLabel           = "/loki/api/v1/label"
	PathLokiLabelNameValues = "/loki/api/v1/label/{name}/values"
	// Index
	PathLokiIndexStats       = "/loki/api/v1/index/stats"
	PathLokiIndexShards      = "/loki/api/v1/index/shards"
	PathLokiIndexVolume      = "/loki/api/v1/index/volume"
	PathLokiIndexVolumeRange = "/loki/api/v1/index/volume_range"
	// Patterns and log metadata
	PathLokiPatterns                = "/loki/api/v1/patterns"
	PathLokiDetectedLabels          = "/loki/api/v1/detected_labels"
	PathLokiDetectedFields          = "/loki/api/v1/detected_fields"
	PathLokiDetectedFieldNameValues = "/loki/api/v1/detected_field/{name}/values"
	// Tail (live tailing)
	PathLokiTail = "/loki/api/v1/tail"
	// Ruler
	PathLokiRules               = "/loki/api/v1/rules"
	PathLokiRulesNamespace      = "/loki/api/v1/rules/{namespace}"
	PathLokiRulesNamespaceGroup = "/loki/api/v1/rules/{namespace}/{groupName}"
	// Delete requests (compactor)
	PathLokiDelete          = "/loki/api/v1/delete"
	PathLokiCacheGenNumbers = "/loki/api/v1/cache/generation_numbers"
	// Ingest
	PathLokiPush = "/loki/api/v1/push"
)

// Prometheus-compatible (legacy) API path constants.
const (
	PathPromQuery           = "/api/prom/query"
	PathPromLabel           = "/api/prom/label"
	PathPromLabelPrefix     = "/api/prom/label/" // prefix for path matching
	PathPromLabelSuffix     = "/values"
	PathPromLabelNameValues = "/api/prom/label/{name}/values"
	PathPromSeries          = "/api/prom/series"
	PathPromPush            = "/api/prom/push"
	PathPromTail            = "/api/prom/tail"
	// Ruler
	PathPromRules               = "/api/prom/rules"
	PathPromRulesNamespace      = "/api/prom/rules/{namespace}"
	PathPromRulesNamespaceGroup = "/api/prom/rules/{namespace}/{groupName}"
)

// Prometheus API paths (used by ruler).
const (
	PathPrometheusRules  = "/prometheus/api/v1/rules"
	PathPrometheusAlerts = "/prometheus/api/v1/alerts"
)
