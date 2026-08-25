package validation

import "github.com/grafana/dskit/flagext"

type BlockedQuery struct {
	Pattern string                 `yaml:"pattern"`
	Regex   bool                   `yaml:"regex"`
	Hash    uint32                 `yaml:"hash"`
	Types   flagext.StringSliceCSV `yaml:"types"`
	// Tags defines a set of key=value constraints that must all match the
	// incoming request tags (from X-Query-Tags) for this rule to apply.
	// Keys are case-insensitive; values are matched case-insensitively.
	Tags map[string]string `yaml:"query_tags"`
}
