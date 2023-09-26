package util

// copy and modification of github.com/prometheus/prometheus/model/relabel/relabel.go
// reason: the custom types in github.com/prometheus/prometheus/model/relabel/relabel.go are difficult to unmarshal
type RelabelConfig struct {
	// A list of labels from which values are taken and concatenated
	// with the configured separator in order.
	SourceLabels []string `yaml:"source_labels,flow,omitempty" json:"source_labels,omitempty"`
	// Separator is the string between concatenated values from the source labels.
	Separator string `yaml:"separator,omitempty" json:"separator,omitempty"`
	// Regex against which the concatenation is matched.
	Regex string `yaml:"regex,omitempty" json:"regex,omitempty"`
	// Modulus to take of the hash of concatenated values from the source labels.
	Modulus uint64 `yaml:"modulus,omitempty" json:"modulus,omitempty"`
	// TargetLabel is the label to which the resulting string is written in a replacement.
	// Regexp interpolation is allowed for the replace action.
	TargetLabel string `yaml:"target_label,omitempty" json:"target_label,omitempty"`
	// Replacement is the regex replacement pattern to be used.
	Replacement string `yaml:"replacement,omitempty" json:"replacement,omitempty"`
	// Action is the action to be performed for the relabeling.
	Action string `yaml:"action,omitempty" json:"action,omitempty"`
}
