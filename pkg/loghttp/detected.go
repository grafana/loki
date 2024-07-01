package loghttp

import "github.com/grafana/loki/v3/pkg/logproto"

// LabelResponse represents the http json response to a label query
type DetectedFieldsResponse struct {
	Fields []DetectedField `json:"fields,omitempty"`
}

type DetectedField struct {
	Label       string                     `json:"label,omitempty"`
	Type        logproto.DetectedFieldType `json:"type,omitempty"`
	Cardinality uint64                     `json:"cardinality,omitempty"`
	Parser      string                     `json:"parser,omitempty"`
}
