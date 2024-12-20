package parquet

import (
	"strings"
)

type columnPath []string

func (path columnPath) append(names ...string) columnPath {
	return append(path[:len(path):len(path)], names...)
}

func (path columnPath) equal(other columnPath) bool {
	return stringsAreEqual(path, other)
}

func (path columnPath) less(other columnPath) bool {
	return stringsAreOrdered(path, other)
}

func (path columnPath) String() string {
	return strings.Join(path, ".")
}

func stringsAreEqual(strings1, strings2 []string) bool {
	if len(strings1) != len(strings2) {
		return false
	}

	for i := range strings1 {
		if strings1[i] != strings2[i] {
			return false
		}
	}

	return true
}

func stringsAreOrdered(strings1, strings2 []string) bool {
	n := len(strings1)

	if n > len(strings2) {
		n = len(strings2)
	}

	for i := 0; i < n; i++ {
		if strings1[i] >= strings2[i] {
			return false
		}
	}

	return len(strings1) <= len(strings2)
}

type leafColumn struct {
	node               Node
	path               columnPath
	maxRepetitionLevel byte
	maxDefinitionLevel byte
	columnIndex        int16
}

func forEachLeafColumnOf(node Node, do func(leafColumn)) {
	forEachLeafColumn(node, nil, 0, 0, 0, do)
}

func forEachLeafColumn(node Node, path columnPath, columnIndex, maxRepetitionLevel, maxDefinitionLevel int, do func(leafColumn)) int {
	switch {
	case node.Optional():
		maxDefinitionLevel++
	case node.Repeated():
		maxRepetitionLevel++
		maxDefinitionLevel++
	}

	if node.Leaf() {
		do(leafColumn{
			node:               node,
			path:               path,
			maxRepetitionLevel: makeRepetitionLevel(maxRepetitionLevel),
			maxDefinitionLevel: makeDefinitionLevel(maxDefinitionLevel),
			columnIndex:        makeColumnIndex(columnIndex),
		})
		return columnIndex + 1
	}

	for _, field := range node.Fields() {
		columnIndex = forEachLeafColumn(
			field,
			path.append(field.Name()),
			columnIndex,
			maxRepetitionLevel,
			maxDefinitionLevel,
			do,
		)
	}

	return columnIndex
}

func lookupColumnPath(node Node, path columnPath) Node {
	for node != nil && len(path) > 0 {
		node = fieldByName(node, path[0])
		path = path[1:]
	}
	return node
}

func hasColumnPath(node Node, path columnPath) bool {
	return lookupColumnPath(node, path) != nil
}
