// Package layout defines the [Layout], which groups [array.Array]s into a
// hierarhical structure.
package layout

import (
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/array"
)

// Layout describes a hierarchy of [array.Arrays]. Each Layout is a grouping of
// other Layouts, with [array.Array]s being leaf nodes.
type Layout interface {
	isLayout() // Sealed marker method.

	// Kind returns the Kind for the Layout.
	Kind() Kind

	// DataType returns the data type of the Layout.
	DataType() types.Type

	// Len returns the total number of rows in the Layout.
	Len() int
}

// Layout definitions.
type (
	// Array is a leaf node holding exactly one Array.
	Array struct {
		Metadata array.Array
	}
)

// Kind returns [KindArray].
func (l *Array) Kind() Kind {
	return KindArray
}

// DataType returns the data type of the Array.
func (l *Array) DataType() types.Type {
	return l.Metadata.Type
}

// Len returns the number of rows in the Array.
func (l *Array) Len() int {
	return l.Metadata.RowCount
}

//
// Sealed marker implementations.
//

func (l *Array) isLayout() {}
