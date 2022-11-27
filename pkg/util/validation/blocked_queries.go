package validation

import "github.com/grafana/dskit/flagext"

type BlockedQuery struct {
	Query string                 `yaml:"query"`
	Regex bool                   `yaml:"regex"`
	Types flagext.StringSliceCSV `yaml:"types"`
}
