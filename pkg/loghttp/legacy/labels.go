package loghttp

// LabelResponse represents the http json response to a label query
type LabelResponse struct {
	Values []string `json:"values,omitempty"`
}
