package engine

import "github.com/grafana/loki/v3/pkg/engine/internal/assertions"

// EnableParanoidMode turns on runtime assertions for execution pipelines that
// will check important invariants on input and output records, such as column
// names uniqueness and labels uniqueness. This affects performance if enabled.
func EnableParanoidMode() {
	assertions.Enabled = true
}
