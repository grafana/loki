package decoder

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

type PathString string

func (s PathString) Build() (*Path, error) {
	builder := new(PathBuilder)
	return builder.Build([]rune(s))
}

type PathBuilder struct {
	root                    PathNode
	node                    PathNode
	singleQuotePathSelector bool
	doubleQuotePathSelector bool
}

func (b *PathBuilder) Build(buf []rune) (*Path, error) {
	node, err := b.build(buf)
	if err != nil {
		return nil, err
	}
	return &Path{
		node:                    node,
		RootSelectorOnly:        node == nil,
		SingleQuotePathSelector: b.singleQuotePathSelector,
		DoubleQuotePathSelector: b.doubleQuotePathSelector,
	}, nil
}

func (b *PathBuilder) build(buf []rune) (PathNode, error) {
	if len(buf) == 0 {
		return nil, errors.ErrEmptyPath()
	}
	if buf[0] != '$' {
		return nil, errors.ErrInvalidPath("JSON Path must start with a $ character")
	}
	if len(buf) == 1 {
		return nil, nil
	}
	buf = buf[1:]
	offset, err := b.buildNext(buf)
	if err != nil {
		return nil, err
	}
	if len(buf) > offset {
		return nil, errors.ErrInvalidPath("remain invalid path %q", buf[offset:])
	}
	return b.root, nil
}

func (b *PathBuilder) buildNextCharIfExists(buf []rune, cursor int) (int, error) {
	if len(buf) > cursor {
		offset, err := b.buildNext(buf[cursor:])
		if err != nil {
			return 0, err
		}
		return cursor + 1 + offset, nil
	}
	return cursor, nil
}

func (b *PathBuilder) buildNext(buf []rune) (int, error) {
	switch buf[0] {
	case '.':
		if len(buf) == 1 {
			return 0, errors.ErrInvalidPath("JSON Path ends with dot character")
		}
		offset, err := b.buildSelector(buf[1:])
		if err != nil {
			return 0, err
		}
		return offset + 1, nil
	case '[':
		if len(buf) == 1 {
			return 0, errors.ErrInvalidPath("JSON Path ends with left bracket character")
		}
		offset, err := b.buildIndex(buf[1:])
		if err != nil {
			return 0, err
		}
		return offset + 1, nil
	default:
		return 0, errors.ErrInvalidPath("expect dot or left bracket character. but found %c character", buf[0])
	}
}

func (b *PathBuilder) buildSelector(buf []rune) (int, error) {
	switch buf[0] {
	case '.':
		if len(buf) == 1 {
			return 0, errors.ErrInvalidPath("JSON Path ends with double dot character")
		}
		offset, err := b.buildPathRecursive(buf[1:])
		if err != nil {
			return 0, err
		}
		return 1 + offset, nil
	case '[', ']', '$', '*':
		return 0, errors.ErrInvalidPath("found invalid path character %c after dot", buf[0])
	}
	for cursor := 0; cursor < len(buf); cursor++ {
		switch buf[cursor] {
		case '$', '*', ']':
			return 0, errors.ErrInvalidPath("found %c character in field selector context", buf[cursor])
		case '.':
			if cursor+1 >= len(buf) {
				return 0, errors.ErrInvalidPath("JSON Path ends with dot character")
			}
			selector := buf[:cursor]
			b.addSelectorNode(string(selector))
			offset, err := b.buildSelector(buf[cursor+1:])
			if err != nil {
				return 0, err
			}
			return cursor + 1 + offset, nil
		case '[':
			if cursor+1 >= len(buf) {
				return 0, errors.ErrInvalidPath("JSON Path ends with left bracket character")
			}
			selector := buf[:cursor]
			b.addSelectorNode(string(selector))
			offset, err := b.buildIndex(buf[cursor+1:])
			if err != nil {
				return 0, err
			}
			return cursor + 1 + offset, nil
		case '"':
			if cursor+1 >= len(buf) {
				return 0, errors.ErrInvalidPath("JSON Path ends with double quote character")
			}
			offset, err := b.buildQuoteSelector(buf[cursor+1:], DoubleQuotePathSelector)
			if err != nil {
				return 0, err
			}
			return cursor + 1 + offset, nil
		}
	}
	b.addSelectorNode(string(buf))
	return len(buf), nil
}

func (b *PathBuilder) buildQuoteSelector(buf []rune, sel QuotePathSelector) (int, error) {
	switch buf[0] {
	case '[', ']', '$', '.', '*', '\'', '"':
		return 0, errors.ErrInvalidPath("found invalid path character %c after quote", buf[0])
	}
	for cursor := 0; cursor < len(buf); cursor++ {
		switch buf[cursor] {
		case '\'':
			if sel != SingleQuotePathSelector {
				return 0, errors.ErrInvalidPath("found double quote character in field selector with single quote context")
			}
			if len(buf) <= cursor+1 {
				return 0, errors.ErrInvalidPath("JSON Path ends with single quote character in field selector context")
			}
			if buf[cursor+1] != ']' {
				return 0, errors.ErrInvalidPath("expect right bracket for field selector with single quote but found %c", buf[cursor+1])
			}
			selector := buf[:cursor]
			b.addSelectorNode(string(selector))
			b.singleQuotePathSelector = true
			return b.buildNextCharIfExists(buf, cursor+2)
		case '"':
			if sel != DoubleQuotePathSelector {
				return 0, errors.ErrInvalidPath("found single quote character in field selector with double quote context")
			}
			selector := buf[:cursor]
			b.addSelectorNode(string(selector))
			b.doubleQuotePathSelector = true
			return b.buildNextCharIfExists(buf, cursor+1)
		}
	}
	return 0, errors.ErrInvalidPath("couldn't find quote character in selector quote path context")
}

func (b *PathBuilder) buildPathRecursive(buf []rune) (int, error) {
	switch buf[0] {
	case '.', '[', ']', '$', '*':
		return 0, errors.ErrInvalidPath("found invalid path character %c after double dot", buf[0])
	}
	for cursor := 0; cursor < len(buf); cursor++ {
		switch buf[cursor] {
		case '$', '*', ']':
			return 0, errors.ErrInvalidPath("found %c character in field selector context", buf[cursor])
		case '.':
			if cursor+1 >= len(buf) {
				return 0, errors.ErrInvalidPath("JSON Path ends with dot character")
			}
			selector := buf[:cursor]
			b.addRecursiveNode(string(selector))
			offset, err := b.buildSelector(buf[cursor+1:])
			if err != nil {
				return 0, err
			}
			return cursor + 1 + offset, nil
		case '[':
			if cursor+1 >= len(buf) {
				return 0, errors.ErrInvalidPath("JSON Path ends with left bracket character")
			}
			selector := buf[:cursor]
			b.addRecursiveNode(string(selector))
			offset, err := b.buildIndex(buf[cursor+1:])
			if err != nil {
				return 0, err
			}
			return cursor + 1 + offset, nil
		}
	}
	b.addRecursiveNode(string(buf))
	return len(buf), nil
}

func (b *PathBuilder) buildIndex(buf []rune) (int, error) {
	switch buf[0] {
	case '.', '[', ']', '$':
		return 0, errors.ErrInvalidPath("found invalid path character %c after left bracket", buf[0])
	case '\'':
		if len(buf) == 1 {
			return 0, errors.ErrInvalidPath("JSON Path ends with single quote character")
		}
		offset, err := b.buildQuoteSelector(buf[1:], SingleQuotePathSelector)
		if err != nil {
			return 0, err
		}
		return 1 + offset, nil
	case '*':
		if len(buf) == 1 {
			return 0, errors.ErrInvalidPath("JSON Path ends with star character")
		}
		if buf[1] != ']' {
			return 0, errors.ErrInvalidPath("expect right bracket character for index all path but found %c character", buf[1])
		}
		b.addIndexAllNode()
		offset := len("*]")
		if len(buf) > 2 {
			buildOffset, err := b.buildNext(buf[2:])
			if err != nil {
				return 0, err
			}
			return offset + buildOffset, nil
		}
		return offset, nil
	}

	for cursor := 0; cursor < len(buf); cursor++ {
		switch buf[cursor] {
		case ']':
			index, err := strconv.ParseInt(string(buf[:cursor]), 10, 64)
			if err != nil {
				return 0, errors.ErrInvalidPath("%q is unexpected index path", buf[:cursor])
			}
			b.addIndexNode(int(index))
			return b.buildNextCharIfExists(buf, cursor+1)
		}
	}
	return 0, errors.ErrInvalidPath("couldn't find right bracket character in index path context")
}

func (b *PathBuilder) addIndexAllNode() {
	node := newPathIndexAllNode()
	if b.root == nil {
		b.root = node
		b.node = node
	} else {
		b.node = b.node.chain(node)
	}
}

func (b *PathBuilder) addRecursiveNode(selector string) {
	node := newPathRecursiveNode(selector)
	if b.root == nil {
		b.root = node
		b.node = node
	} else {
		b.node = b.node.chain(node)
	}
}

func (b *PathBuilder) addSelectorNode(name string) {
	node := newPathSelectorNode(name)
	if b.root == nil {
		b.root = node
		b.node = node
	} else {
		b.node = b.node.chain(node)
	}
}

func (b *PathBuilder) addIndexNode(idx int) {
	node := newPathIndexNode(idx)
	if b.root == nil {
		b.root = node
		b.node = node
	} else {
		b.node = b.node.chain(node)
	}
}

type QuotePathSelector int

const (
	SingleQuotePathSelector QuotePathSelector = 1
	DoubleQuotePathSelector QuotePathSelector = 2
)

type Path struct {
	node                    PathNode
	RootSelectorOnly        bool
	SingleQuotePathSelector bool
	DoubleQuotePathSelector bool
}

func (p *Path) Field(sel string) (PathNode, bool, error) {
	if p.node == nil {
		return nil, false, nil
	}
	return p.node.Field(sel)
}

func (p *Path) Get(src, dst reflect.Value) error {
	if p.node == nil {
		return nil
	}
	return p.node.Get(src, dst)
}

func (p *Path) String() string {
	if p.node == nil {
		return "$"
	}
	return p.node.String()
}

type PathNode interface {
	fmt.Stringer
	Index(idx int) (PathNode, bool, error)
	Field(fieldName string) (PathNode, bool, error)
	Get(src, dst reflect.Value) error
	chain(PathNode) PathNode
	// single reports whether Get of the path from the node on gives one value: no node of it is [*] or .., whose Get
	// gives a slice of all the values they select.
	single() bool
}

type BasePathNode struct {
	child PathNode
}

func (n *BasePathNode) chain(node PathNode) PathNode {
	n.child = node
	return node
}

func (n *BasePathNode) single() bool {
	return n.child == nil || n.child.single()
}

// pathValue returns the value which v holds through pointers and interfaces, followed in a loop, or the zero Value if
// one of them is nil: null, which has no member or element. It returns an error if a pointer leads back to itself
// ( see pointerVisits ).
func pathValue(v reflect.Value) (reflect.Value, error) {
	var pointers pointerVisits
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, nil
		}
		if v.Kind() == reflect.Ptr {
			if err := pointers.follow(v); err != nil {
				return reflect.Value{}, err
			}
		}
		v = v.Elem()
	}
	return v, nil
}

// pathTypeName names the type of v, a value from pathValue, in an error.
func pathTypeName(v reflect.Value) string {
	if !v.IsValid() {
		return "null"
	}
	return v.Type().String()
}

// isPathField reports whether a path selects field, of a struct, by its name: whether the JSON object of the struct
// has the field and its value can be read. An unexported field has not, nor a field tagged "-".
func isPathField(field reflect.StructField) bool {
	return field.IsExported() && !runtime.IsIgnoredStructField(field)
}

// isUnsupportedValue reports whether err, of Get, is that the value cannot be gone through, as it holds itself or is
// nested too deep ( see visits ): [*] and .. report it, where they leave out a value in which the rest of the path
// selects nothing.
func isUnsupportedValue(err error) bool {
	_, ok := err.(*errors.UnsupportedValueError)
	return ok
}

// mapKeyName returns the name which key, of a map, is in its JSON object, if it is of a string kind, or an interface
// which holds one. A path selects no other key by a name.
func mapKeyName(key reflect.Value) (string, bool) {
	if key.Kind() == reflect.Interface {
		key = key.Elem()
	}
	if key.Kind() != reflect.String {
		return "", false
	}
	return key.String(), true
}

// appendPathValue appends v, which Get of the path from child on gave, to values: the values in v one by one if that
// path gives a slice of them ( see PathNode.single ).
func appendPathValue(values []any, v any, child PathNode) []any {
	if child == nil || child.single() {
		return append(values, v)
	}
	selected, _ := v.([]any)
	return append(values, selected...)
}

type PathSelectorNode struct {
	*BasePathNode
	selector string
}

func newPathSelectorNode(selector string) *PathSelectorNode {
	return &PathSelectorNode{
		BasePathNode: &BasePathNode{},
		selector:     selector,
	}
}

func (n *PathSelectorNode) Index(idx int) (PathNode, bool, error) {
	return nil, false, &errors.PathError{}
}

func (n *PathSelectorNode) Field(fieldName string) (PathNode, bool, error) {
	if n.selector == fieldName {
		return n.child, true, nil
	}
	return nil, false, nil
}

func (n *PathSelectorNode) Get(src, dst reflect.Value) error {
	src, err := pathValue(src)
	if err != nil {
		return err
	}
	switch src.Kind() {
	case reflect.Map:
		iter := src.MapRange()
		for iter.Next() {
			key, ok := mapKeyName(iter.Key())
			if !ok {
				continue
			}
			child, found, err := n.Field(key)
			if err != nil {
				return err
			}
			if found {
				if child != nil {
					return child.Get(iter.Value(), dst)
				}
				return AssignValue(iter.Value(), dst)
			}
		}
	case reflect.Struct:
		typ := src.Type()
		for i := 0; i < typ.NumField(); i++ {
			if !isPathField(typ.Field(i)) {
				continue
			}
			tag := runtime.StructTagFromField(typ.Field(i))
			child, found, err := n.Field(tag.Key)
			if err != nil {
				return err
			}
			if found {
				if child != nil {
					return child.Get(src.Field(i), dst)
				}
				return AssignValue(src.Field(i), dst)
			}
		}
	}
	return fmt.Errorf("failed to get %s value from %s", n.selector, pathTypeName(src))
}

func (n *PathSelectorNode) String() string {
	s := fmt.Sprintf(".%s", n.selector)
	if n.child != nil {
		s += n.child.String()
	}
	return s
}

type PathIndexNode struct {
	*BasePathNode
	selector int
}

func newPathIndexNode(selector int) *PathIndexNode {
	return &PathIndexNode{
		BasePathNode: &BasePathNode{},
		selector:     selector,
	}
}

func (n *PathIndexNode) Index(idx int) (PathNode, bool, error) {
	if n.selector == idx {
		return n.child, true, nil
	}
	return nil, false, nil
}

func (n *PathIndexNode) Field(fieldName string) (PathNode, bool, error) {
	return nil, false, &errors.PathError{}
}

func (n *PathIndexNode) Get(src, dst reflect.Value) error {
	src, err := pathValue(src)
	if err != nil {
		return err
	}
	switch src.Kind() {
	case reflect.Array, reflect.Slice:
		if n.selector >= 0 && n.selector < src.Len() {
			if n.child != nil {
				return n.child.Get(src.Index(n.selector), dst)
			}
			return AssignValue(src.Index(n.selector), dst)
		}
	}
	return fmt.Errorf("failed to get [%d] value from %s", n.selector, pathTypeName(src))
}

func (n *PathIndexNode) String() string {
	s := fmt.Sprintf("[%d]", n.selector)
	if n.child != nil {
		s += n.child.String()
	}
	return s
}

type PathIndexAllNode struct {
	*BasePathNode
}

func newPathIndexAllNode() *PathIndexAllNode {
	return &PathIndexAllNode{
		BasePathNode: &BasePathNode{},
	}
}

func (n *PathIndexAllNode) Index(idx int) (PathNode, bool, error) {
	return n.child, true, nil
}

func (n *PathIndexAllNode) Field(fieldName string) (PathNode, bool, error) {
	return nil, false, &errors.PathError{}
}

func (n *PathIndexAllNode) single() bool {
	return false
}

func (n *PathIndexAllNode) Get(src, dst reflect.Value) error {
	src, err := pathValue(src)
	if err != nil {
		return err
	}
	switch src.Kind() {
	case reflect.Array, reflect.Slice:
		var arr []any
		for i := 0; i < src.Len(); i++ {
			var v any
			rv := reflect.ValueOf(&v)
			if n.child != nil {
				if err := n.child.Get(src.Index(i), rv); err != nil {
					if isUnsupportedValue(err) {
						return err
					}
					// the rest of the path selects nothing in the element
					continue
				}
			} else {
				if err := AssignValue(src.Index(i), rv); err != nil {
					return err
				}
			}
			arr = appendPathValue(arr, v, n.child)
		}
		return AssignValue(reflect.ValueOf(arr), dst)
	}
	return fmt.Errorf("failed to get all value from %s", pathTypeName(src))
}

func (n *PathIndexAllNode) String() string {
	s := "[*]"
	if n.child != nil {
		s += n.child.String()
	}
	return s
}

type PathRecursiveNode struct {
	*BasePathNode
	selector string
}

func newPathRecursiveNode(selector string) *PathRecursiveNode {
	return &PathRecursiveNode{
		BasePathNode: &BasePathNode{},
		selector:     selector,
	}
}

func (n *PathRecursiveNode) Field(fieldName string) (PathNode, bool, error) {
	if n.selector == fieldName {
		return n.child, true, nil
	}
	return nil, false, nil
}

func (n *PathRecursiveNode) Index(_ int) (PathNode, bool, error) {
	return n, true, nil
}

func (n *PathRecursiveNode) single() bool {
	return false
}

// Get assigns to dst a slice of the values which the name of n has at any depth of src, in the members of objects
// and the elements of arrays, with the rest of the path applied to each of them: a value in which the rest finds
// nothing adds nothing.
func (n *PathRecursiveNode) Get(src, dst reflect.Value) error {
	var values []any
	if err := n.collect(src, &values, &visits{}); err != nil {
		return err
	}
	return AssignValue(reflect.ValueOf(values), dst)
}

// collect appends to values what Get selects in v.
func (n *PathRecursiveNode) collect(v reflect.Value, values *[]any, seen *visits) error {
	v, err := pathValue(v)
	if err != nil {
		return err
	}
	switch v.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
	default:
		return nil
	}
	if err := seen.enter(v); err != nil {
		return err
	}
	defer seen.leave(v)
	return n.collectContents(v, values, seen)
}

// collectContents is collect of v, an object or an array: a map, a struct, a slice or an array.
func (n *PathRecursiveNode) collectContents(v reflect.Value, values *[]any, seen *visits) error {
	switch v.Kind() {
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if key, ok := mapKeyName(iter.Key()); ok && key == n.selector {
				var err error
				if *values, err = n.appendSelected(*values, iter.Value()); err != nil {
					return err
				}
			}
			if err := n.collect(iter.Value(), values, seen); err != nil {
				return err
			}
		}
	case reflect.Struct:
		typ := v.Type()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !isPathField(field) {
				continue
			}
			tag := runtime.StructTagFromField(field)
			if tag.Key == n.selector {
				var err error
				if *values, err = n.appendSelected(*values, v.Field(i)); err != nil {
					return err
				}
			}
			collect := n.collect
			if field.Anonymous && field.Type.Kind() == reflect.Struct && !tag.IsTaggedKey {
				// a struct embedded by its value, with no name in its tag, is a part of the object of v, as Marshal
				// writes it, not a level
				collect = n.collectContents
			}
			if err := collect(v.Field(i), values, seen); err != nil {
				return err
			}
		}
	default:
		for i := 0; i < v.Len(); i++ {
			if err := n.collect(v.Index(i), values, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendSelected appends to values v, a value of the name of n, with the rest of the path applied to it: nothing if
// the rest selects nothing in it.
func (n *PathRecursiveNode) appendSelected(values []any, v reflect.Value) ([]any, error) {
	if n.child == nil {
		return append(values, v.Interface()), nil
	}
	var selected any
	if err := n.child.Get(v, reflect.ValueOf(&selected)); err != nil {
		if isUnsupportedValue(err) {
			return nil, err
		}
		return values, nil
	}
	return appendPathValue(values, selected, n.child), nil
}

func (n *PathRecursiveNode) String() string {
	s := fmt.Sprintf("..%s", n.selector)
	if n.child != nil {
		s += n.child.String()
	}
	return s
}
