package stats_test

import (
	"strings"

	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
)

// testLayout returns the layout with the default shard layout and the schema
// labels of sortSchema.
func testLayout(sortSchema string) streams.Layout {
	var schemaLabels []string
	if sortSchema != "" {
		schemaLabels = strings.Split(sortSchema, ",")
	}
	return streams.NewLayout(streams.DefaultShardLayout(), schemaLabels)
}
