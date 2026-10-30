package dataset

import (
	"github.com/grafana/loki/pkg/columnar/types"
	"github.com/grafana/loki/pkg/dataset/layout"
)

// A Dataset holds a collection of rows with a fixed schema.
type Dataset struct {
	Type   types.Type
	Layout layout.Layout
}
