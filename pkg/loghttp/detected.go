package loghttp

import (
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
)

// DetectedFieldsResponse represents the JSON response to a detected-field or field-value query.
type DetectedFieldsResponse struct {
	Fields     []DetectedField `json:"fields,omitempty"`
	Values     []string        `json:"values,omitempty"`
	Statistics stats.Result    `json:"stats"`
}

type DetectedField struct {
	Label       string                     `json:"label,omitempty"`
	Type        logproto.DetectedFieldType `json:"type,omitempty"`
	Cardinality uint64                     `json:"cardinality,omitempty"`
	Parsers     []string                   `json:"parsers,omitempty"`
}
