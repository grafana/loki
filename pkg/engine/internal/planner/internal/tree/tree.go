package tree

// Property represents a property of a [Node]. It is a key-value-pair, where
// the value is either a single value or a list of values.
// When the value is a multi-value, the field IsMultiValue needs to be set to
// `true`.
// A single-value property is represented as `key=value` and a multi-value
// property as `key=(value1, value2, ...)`.
type Property struct {
	// Key is the name of the property.
	Key string
	// Values holds the value(s) of the property.
	Values []any
	// IsMultiValue marks whether the property is a multi-value property.
	IsMultiValue bool
}

// NewProperty creates a new Property with the specified key, multi-value flag, and values.
// The multi parameter determines if the property should be treated as a multi-value property.
func NewProperty(key string, multi bool, values ...any) Property {
	return Property{
		Key:          key,
		Values:       values,
		IsMultiValue: multi,
	}
}

// Node represents a node in a tree structure that can be traversed and printed
// by the [Printer].
// It allows for building hierarchical representations of data where each node
// can have multiple properties and multiple children.
type Node struct {
	// ID is a unique identifier for the node.
	ID string
	// Name is the display name of the node.
	Name string
	// Properties contains a list of key-value properties associated with the node.
	Properties []Property
	// Children are child nodes of the node.
	Children []*Node
	// Comments, like Children, are child nodes of the node, with the difference
	// that comments are indented a level deeper than children. A common use-case
	// for comments are tree-style properties of a node, such as expressions of a
	// physical plan node.
	Comments []*Node
}

// NewNode creates a new node with the given name, unique identifier and
// properties.
func NewNode(name, id string, properties ...Property) *Node {
	return &Node{
		ID:         id,
		Name:       name,
		Properties: properties,
	}
}

// AddChild creates a new node with the given name, unique identifier, and properties
// and adds it to the parent node.
func (n *Node) AddChild(name, id string, properties []Property) *Node {
	child := NewNode(name, id, properties...)
	n.Children = append(n.Children, child)
	return child
}

func (n *Node) AddComment(name, id string, properties []Property) *Node {
	node := NewNode(name, id, properties...)
	n.Comments = append(n.Comments, node)
	return node
}
