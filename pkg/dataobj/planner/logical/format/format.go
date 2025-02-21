package format

import "strings"

// Format represents something that can describe itself as a formatted node
// and its children in a tree
type Format interface {
	Format(f Formatter)
}

// Formatter controls how nodes are formatted and maintains the format tree
type Formatter interface {
	// WriteNode starts a new node in the format tree and returns a new Formatter
	// targeting the newly created node.
	// This allows for adding children to the newly created node later
	WriteNode(node Node) Formatter
	// Format renders the complete format tree
	Format() string
}

// An abstract formatted node
// in the tree. All of these are formatted with the same indent, but
// we allow for different _types_ of entities, e.g. Singletons, tuples, etc
type Node struct {
	// e.g. "SELECT"
	Singletons []string
	// e.g. "GROUP BY (Foo, Bar)",
	// "foo=bar"
	Tuples []ContentTuple
}

type Content interface {
	Content() string
}

type ContentTuple struct {
	Key   string
	Value Content
}

func (t ContentTuple) Content() string {
	var sb strings.Builder
	sb.WriteString(t.Key)
	sb.WriteString("=")
	sb.WriteString(t.Value.Content())
	return sb.String()
}

type ListContent []Content

func ListContentFrom(values ...string) ListContent {
	var contents []Content
	for _, value := range values {
		contents = append(contents, SingleContent(value))
	}
	return ListContent(contents)
}

func (g ListContent) Content() string {
	var sb strings.Builder
	sb.WriteString("(")
	for i, c := range g {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(c.Content())
	}
	sb.WriteString(")")
	return sb.String()
}

type SingleContent string

func (s SingleContent) Content() string {
	return string(s)
}
