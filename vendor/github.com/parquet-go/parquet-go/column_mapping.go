package parquet

// LeafColumn is a struct type representing leaf columns of a parquet schema.
type LeafColumn struct {
	Node               Node
	Path               []string
	ColumnIndex        int
	MaxRepetitionLevel int
	MaxDefinitionLevel int
}

func columnMappingOf(schema Node) (mapping columnMappingGroup, columns [][]string) {
	mapping = make(columnMappingGroup)
	columns = make([][]string, 0, 16)

	forEachLeafColumnOf(schema, func(leaf leafColumn) {
		path := make(columnPath, len(leaf.path))
		copy(path, leaf.path)
		columns = append(columns, path)

		group := mapping
		for len(path) > 1 {
			columnName := path[0]
			g, ok := group[columnName].(columnMappingGroup)
			if !ok {
				g = make(columnMappingGroup)
				group[columnName] = g
			}
			group, path = g, path[1:]
		}

		leaf.path = path // use the copy
		group[path[0]] = &columnMappingLeaf{column: leaf}
	})

	return mapping, columns
}

type columnMapping interface {
	lookup(path columnPath) leafColumn
}

type columnMappingGroup map[string]columnMapping

func (group columnMappingGroup) lookup(path columnPath) leafColumn {
	if len(path) > 0 {
		c, ok := group[path[0]]
		if ok {
			return c.lookup(path[1:])
		}
	}
	return leafColumn{columnIndex: -1}
}

func (group columnMappingGroup) lookupClosest(path columnPath) leafColumn {
	for len(path) > 0 {
		g, ok := group[path[0]].(columnMappingGroup)
		if ok {
			group, path = g, path[1:]
		} else {
			firstName := ""
			firstLeaf := (*columnMappingLeaf)(nil)
			for name, child := range group {
				if leaf, ok := child.(*columnMappingLeaf); ok {
					if firstLeaf == nil || name < firstName {
						firstName, firstLeaf = name, leaf
					}
				}
			}
			if firstLeaf != nil {
				return firstLeaf.column
			}
			break
		}
	}
	return leafColumn{columnIndex: -1}
}

type columnMappingLeaf struct {
	column leafColumn
}

func (leaf *columnMappingLeaf) lookup(path columnPath) leafColumn {
	if len(path) == 0 {
		return leaf.column
	}
	return leafColumn{columnIndex: -1}
}
