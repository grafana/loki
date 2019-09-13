package v1

//LabelResponse represents the http json response to a label query
type LabelResponse struct {
	Values []string `json:"values,omitempty"`
}

//Labels is a key/value pair mapping of labels
type LabelSet map[string]string
