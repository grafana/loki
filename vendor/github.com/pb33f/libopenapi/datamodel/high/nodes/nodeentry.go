package nodes

import "github.com/pb33f/go-yaml"

// NodeEntry represents a single node used by NodeBuilder.
type NodeEntry struct {
	Tag         string
	Key         string
	Value       any
	StringValue string
	Line        int
	KeyStyle    yaml.Style
	// ValueStyle  yaml.Style
	RenderZero bool
	LowValue   any
}
