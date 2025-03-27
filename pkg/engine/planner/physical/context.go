package physical

import "github.com/grafana/loki/v3/pkg/dataobj/metastore"

// Catalog is an interface that provides methods for interacting with
// storage metadata. In traditional database systems there are system tables
// providing this information (e.g. pg_catalog, ...) whereas in Loki there
// is the Metastore.
type Catalog interface {
	ResolveDataObj(Expression) ([]DataObjLocation, [][]int64)
}

// Context is the default implementation of [Catalog].
type Context struct {
	metastore metastore.Metastore
}

// ResolveDataObj resolves DataObj locations and streams IDs based on a given
// [Expression]. The expression is required to be a (tree of) [BinaryExpression]
// with a [ColumnExpression] on the left and a [LiteralExpression] on the right.
func (c *Context) ResolveDataObj(_ Expression) ([]DataObjLocation, [][]int64) {
	panic("not implemented")
}

var _ Catalog = (*Context)(nil)
