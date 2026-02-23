package jsonpath

import (
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// FilterContext provides rich context during filter evaluation for JSONPath Plus extensions.
type FilterContext interface {
	index

	PropertyName() string
	SetPropertyName(name string)

	Parent() *yaml.Node
	SetParent(parent *yaml.Node)

	ParentPropertyName() string
	SetParentPropertyName(name string)

	Path() string
	PushPathSegment(segment string)
	PopPathSegment()

	// SetPendingPathSegment stores a path segment for a node (used by wildcards/slices)
	SetPendingPathSegment(node *yaml.Node, segment string)
	// GetAndClearPendingPathSegment retrieves and removes a pending path segment for a node
	GetAndClearPendingPathSegment(node *yaml.Node) string

	// SetPendingPropertyName stores a property name for a node (used by wildcards for @parentProperty)
	SetPendingPropertyName(node *yaml.Node, name string)
	// GetAndClearPendingPropertyName retrieves and removes a pending property name for a node
	GetAndClearPendingPropertyName(node *yaml.Node) string

	Root() *yaml.Node
	SetRoot(root *yaml.Node)

	Index() int
	SetIndex(idx int)

	// EnableParentTracking enables parent node tracking (for ^ and @parent)
	EnableParentTracking()
	// ParentTrackingEnabled returns true if parent tracking is active
	ParentTrackingEnabled() bool

	Clone() FilterContext
}

// filterContext is the concrete implementation of FilterContext
type filterContext struct {
	_index

	propertyName          string
	parent                *yaml.Node
	parentPropertyName    string
	pathSegments          []string
	pendingPathSegments   map[*yaml.Node]string // tracks path segments for nodes from wildcards/slices
	pendingPropertyNames  map[*yaml.Node]string // tracks property names for nodes from wildcards (for @parentProperty)
	root                  *yaml.Node
	arrayIndex            int
	parentTrackingActive  bool
}

// NewFilterContext creates a new FilterContext with the given root node
func NewFilterContext(root *yaml.Node) FilterContext {
	return &filterContext{
		_index: _index{
			propertyKeys: make(map[*yaml.Node]*yaml.Node),
			parentNodes:  make(map[*yaml.Node]*yaml.Node),
		},
		pathSegments:         make([]string, 0),
		pendingPathSegments:  make(map[*yaml.Node]string),
		pendingPropertyNames: make(map[*yaml.Node]string),
		root:                 root,
		arrayIndex:           -1,
	}
}

// PropertyName returns the current property name or array index as string
func (fc *filterContext) PropertyName() string {
	return fc.propertyName
}

// SetPropertyName sets the current property name
func (fc *filterContext) SetPropertyName(name string) {
	fc.propertyName = name
}

// Parent returns the parent node
func (fc *filterContext) Parent() *yaml.Node {
	return fc.parent
}

// SetParent sets the parent node
func (fc *filterContext) SetParent(parent *yaml.Node) {
	fc.parent = parent
}

// ParentPropertyName returns the parent's property name
func (fc *filterContext) ParentPropertyName() string {
	return fc.parentPropertyName
}

// SetParentPropertyName sets the parent's property name
func (fc *filterContext) SetParentPropertyName(name string) {
	fc.parentPropertyName = name
}

// Path returns the normalized JSONPath to the current node
func (fc *filterContext) Path() string {
	if len(fc.pathSegments) == 0 {
		return "$"
	}
	return "$" + strings.Join(fc.pathSegments, "")
}

// PushPathSegment adds a path segment (should be in normalized form like "['key']" or "[0]")
func (fc *filterContext) PushPathSegment(segment string) {
	fc.pathSegments = append(fc.pathSegments, segment)
}

// PopPathSegment removes the last path segment
func (fc *filterContext) PopPathSegment() {
	if len(fc.pathSegments) > 0 {
		fc.pathSegments = fc.pathSegments[:len(fc.pathSegments)-1]
	}
}

// SetPendingPathSegment stores a path segment for a node (used by wildcards/slices)
func (fc *filterContext) SetPendingPathSegment(node *yaml.Node, segment string) {
	if fc.pendingPathSegments != nil {
		fc.pendingPathSegments[node] = segment
	}
}

// GetAndClearPendingPathSegment retrieves and removes a pending path segment for a node
func (fc *filterContext) GetAndClearPendingPathSegment(node *yaml.Node) string {
	if fc.pendingPathSegments == nil {
		return ""
	}
	segment, ok := fc.pendingPathSegments[node]
	if ok {
		delete(fc.pendingPathSegments, node)
		return segment
	}
	return ""
}

// SetPendingPropertyName stores a property name for a node (used by wildcards for @parentProperty)
func (fc *filterContext) SetPendingPropertyName(node *yaml.Node, name string) {
	if fc.pendingPropertyNames != nil {
		fc.pendingPropertyNames[node] = name
	}
}

// GetAndClearPendingPropertyName retrieves and removes a pending property name for a node
func (fc *filterContext) GetAndClearPendingPropertyName(node *yaml.Node) string {
	if fc.pendingPropertyNames == nil {
		return ""
	}
	name, ok := fc.pendingPropertyNames[node]
	if ok {
		delete(fc.pendingPropertyNames, node)
		return name
	}
	return ""
}

// Root returns the root node for @root access
func (fc *filterContext) Root() *yaml.Node {
	return fc.root
}

// SetRoot sets the root node
func (fc *filterContext) SetRoot(root *yaml.Node) {
	fc.root = root
}

// Index returns the current array index (-1 if not in array context)
func (fc *filterContext) Index() int {
	return fc.arrayIndex
}

// SetIndex sets the current array index
func (fc *filterContext) SetIndex(idx int) {
	fc.arrayIndex = idx
}

// EnableParentTracking enables parent node tracking for ^ and @parent
func (fc *filterContext) EnableParentTracking() {
	fc.parentTrackingActive = true
}

// ParentTrackingEnabled returns true if parent tracking is active
func (fc *filterContext) ParentTrackingEnabled() bool {
	return fc.parentTrackingActive
}

// Clone creates a shallow copy of the context for nested evaluation
func (fc *filterContext) Clone() FilterContext {
	pathCopy := make([]string, len(fc.pathSegments))
	copy(pathCopy, fc.pathSegments)

	// Share the pending maps - they're cleared on use anyway
	return &filterContext{
		_index:               fc._index,
		propertyName:         fc.propertyName,
		parent:               fc.parent,
		parentPropertyName:   fc.parentPropertyName,
		pathSegments:         pathCopy,
		pendingPathSegments:  fc.pendingPathSegments,
		pendingPropertyNames: fc.pendingPropertyNames,
		root:                 fc.root,
		arrayIndex:           fc.arrayIndex,
		parentTrackingActive: fc.parentTrackingActive,
	}
}

// Helper function to create a normalized path segment for a property name
func normalizePathSegment(name string) string {
	return "['" + escapePathSegment(name) + "']"
}

// Helper function to create a normalized path segment for an array index
func normalizeIndexSegment(idx int) string {
	return "[" + strconv.Itoa(idx) + "]"
}

// escapePathSegment escapes special characters in path segment names
func escapePathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString("\\'")
		case '\\':
			b.WriteString("\\\\")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
