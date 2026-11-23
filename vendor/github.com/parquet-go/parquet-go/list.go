package parquet

import "reflect"

// List logical types follow the backward-compatibility rules in
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#backward-compatibility-rules
//
// Writers in this package always emit the standard 3-level layout, with a
// repeated group named "list" and an element field named "element". Readers
// accept the other shapes those rules describe.

// listLogicalElement returns the logical element of a LIST node.
//
// elemName is the element's name when it is itself a LIST (rules 3 and 4),
// so a nested list can apply the "<name>_tuple" rule. The returned node has
// the element's own repetition: required for rules 1-4, and the repeated
// field's child repetition for rule 5.
func listLogicalElement(list Node, listName string) (elem Node, elemName string, ok bool) {
	if list == nil || list.Leaf() || !isList(list) {
		return nil, "", false
	}
	fields := list.Fields()
	if len(fields) != 1 || !fields[0].Repeated() {
		return nil, "", false
	}
	repeated := fields[0]
	repName := repeated.Name()

	// Rule 1: a repeated primitive is a required element.
	if repeated.Leaf() {
		return Required(repeated), repName, true
	}

	children := repeated.Fields()
	switch len(children) {
	case 0:
		return nil, "", false
	case 1:
		child := children[0]
		// Rule 3: the only field is repeated, so the group is a required element.
		// Rule 4: a group named "array" or "<list>_tuple" is a required element
		// even when it has a single non-repeated field.
		if child.Repeated() || repName == "array" || (listName != "" && repName == listName+"_tuple") {
			return Required(repeated), repName, true
		}
		// Rule 5: the single child is the element and keeps its repetition.
		return child, child.Name(), true
	default:
		// Rule 2: a repeated group with multiple fields is a required element.
		return Required(repeated), repName, true
	}
}

// nodeView is a schema node with replacement children. It keeps the wrapped
// node's repetition and logical type, so rewriting a LIST does not change the
// definition or repetition levels of its leaves.
type nodeView struct {
	Node
	fields []Field
}

func (n *nodeView) Fields() []Field { return n.fields }

type namedNode struct {
	Node
	name string
}

func (n namedNode) Name() string { return n.name }

func (n namedNode) Value(reflect.Value) reflect.Value { return reflect.Value{} }

func asField(name string, node Node) Field {
	if field, ok := node.(Field); ok && field.Name() == name {
		return field
	}
	return namedNode{Node: node, name: name}
}

// normalizeLists rewrites LIST nodes into the standard 3-level layout
// (repeated group "list", element field "element"). Leaf order and
// repetition and definition levels are preserved; only the grouping and
// field names of list wrappers change.
func normalizeLists(node Node, name string) Node {
	if node == nil {
		return nil
	}
	if _, ok := node.(*nodeView); ok {
		return node
	}
	if isList(node) {
		elem, elemName, ok := listLogicalElement(node, name)
		if !ok {
			return node
		}
		return &nodeView{
			Node: node,
			fields: []Field{
				namedNode{
					name: "list",
					Node: Repeated(Group{"element": normalizeLists(elem, elemName)}),
				},
			},
		}
	}
	if node.Leaf() {
		return node
	}

	fields := node.Fields()
	if len(fields) == 0 {
		return node
	}
	rewritten := make([]Field, len(fields))
	changed := false
	for i, field := range fields {
		child := normalizeLists(field, field.Name())
		rewritten[i] = asField(field.Name(), child)
		if child != field {
			changed = true
		}
	}
	if !changed {
		return node
	}
	return &nodeView{Node: node, fields: rewritten}
}

// normalizedLists is a lazy standard-layout view of a schema, used when a
// target column path is absent because a LIST used a legacy shape.
type normalizedLists struct {
	ready   bool
	root    Node
	mapping columnMappingGroup
}

func (n *normalizedLists) lookup(schema Node, path columnPath) (leafColumn, Node, bool) {
	if !n.ready {
		n.ready = true
		name := ""
		switch s := schema.(type) {
		case *Schema:
			name = s.Name()
		case Field:
			name = s.Name()
		}
		root := normalizeLists(schema, name)
		if root != schema {
			mapping, _ := columnMappingOf(root)
			n.root = root
			n.mapping = mapping
		}
	}
	if n.mapping == nil {
		return leafColumn{}, nil, false
	}
	col := n.mapping.lookup(path)
	if col.node == nil {
		return leafColumn{}, nil, false
	}
	return col, n.root, true
}
