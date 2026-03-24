// Copyright 2011-2019 Canonical Ltd
// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// Constructor stage: Converts YAML nodes to Go values.
// Handles type resolution, custom unmarshalers, and struct field mapping.

package libyaml

import (
	"encoding"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
)

// --------------------------------------------------------------------------
// Interfaces and types needed by constructor

// constructor interface may be implemented by types to customize their
// behavior when being constructed from a YAML document.
type constructor interface {
	UnmarshalYAML(value *Node) error
}

type obsoleteConstructor interface {
	UnmarshalYAML(construct func(any) error) error
}

// Marshaler interface may be implemented by types to customize their
// behavior when being marshaled into a YAML document.
type Marshaler interface {
	MarshalYAML() (any, error)
}

// IsZeroer is used to check whether an object is zero to determine whether
// it should be omitted when marshaling with the ,omitempty flag. One notable
// implementation is time.Time.
type IsZeroer interface {
	IsZero() bool
}

// handleErr recovers from panics caused by yaml errors
func handleErr(err *error) {
	if v := recover(); v != nil {
		if e, ok := v.(*YAMLError); ok {
			*err = e.Err
		} else {
			panic(v)
		}
	}
}

// --------------------------------------------------------------------------
// Struct field information

type structInfo struct {
	FieldsMap  map[string]fieldInfo
	FieldsList []fieldInfo

	// InlineMap is the number of the field in the struct that
	// contains an ,inline map, or -1 if there's none.
	InlineMap int

	// InlineConstructors holds indexes to inlined fields that
	// contain constructor values.
	InlineConstructors [][]int
}

type fieldInfo struct {
	Key       string
	Num       int
	OmitEmpty bool
	Flow      bool
	// Id holds the unique field identifier, so we can cheaply
	// check for field duplicates without maintaining an extra map.
	Id int

	// Inline holds the field index if the field is part of an inlined struct.
	Inline []int
}

var (
	structMap       = make(map[reflect.Type]*structInfo)
	fieldMapMutex   sync.RWMutex
	constructorType reflect.Type
)

func init() {
	var v constructor
	constructorType = reflect.ValueOf(&v).Elem().Type()
}

// hasConstructYAMLMethod checks if a type has an UnmarshalYAML method
// that looks like it implements yaml.Unmarshaler (from root package).
// This is needed because we can't directly check for the interface type
// since it's in a different package that we can't import.
func hasConstructYAMLMethod(t reflect.Type) bool {
	method, found := t.MethodByName("UnmarshalYAML")
	if !found {
		return false
	}

	// Check signature: func(*T) UnmarshalYAML(*Node) error
	mtype := method.Type
	if mtype.NumIn() != 2 || mtype.NumOut() != 1 {
		return false
	}

	// First param is receiver (already checked by MethodByName)
	// Second param should be a pointer to a Node-like struct
	paramType := mtype.In(1)
	if paramType.Kind() != reflect.Ptr {
		return false
	}

	elemType := paramType.Elem()
	if elemType.Kind() != reflect.Struct || elemType.Name() != "Node" {
		return false
	}

	// Return type should be error
	retType := mtype.Out(0)
	if retType.Kind() != reflect.Interface || retType.Name() != "error" {
		return false
	}

	return true
}

func getStructInfo(st reflect.Type) (*structInfo, error) {
	fieldMapMutex.RLock()
	sinfo, found := structMap[st]
	fieldMapMutex.RUnlock()
	if found {
		return sinfo, nil
	}

	n := st.NumField()
	fieldsMap := make(map[string]fieldInfo)
	fieldsList := make([]fieldInfo, 0, n)
	inlineMap := -1
	inlineConstructors := [][]int(nil)
	for i := 0; i != n; i++ {
		field := st.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue // Private field
		}

		info := fieldInfo{Num: i}

		tag := field.Tag.Get("yaml")
		if tag == "" && !strings.Contains(string(field.Tag), ":") {
			tag = string(field.Tag)
		}
		if tag == "-" {
			continue
		}

		inline := false
		fields := strings.Split(tag, ",")
		if len(fields) > 1 {
			for _, flag := range fields[1:] {
				switch flag {
				case "omitempty":
					info.OmitEmpty = true
				case "flow":
					info.Flow = true
				case "inline":
					inline = true
				default:
					return nil, fmt.Errorf("unsupported flag %q in tag %q of type %s", flag, tag, st)
				}
			}
			tag = fields[0]
		}

		if inline {
			switch field.Type.Kind() {
			case reflect.Map:
				if inlineMap >= 0 {
					return nil, errors.New("multiple ,inline maps in struct " + st.String())
				}
				if field.Type.Key() != reflect.TypeOf("") {
					return nil, errors.New("option ,inline needs a map with string keys in struct " + st.String())
				}
				inlineMap = info.Num
			case reflect.Struct, reflect.Pointer:
				ftype := field.Type
				for ftype.Kind() == reflect.Pointer {
					ftype = ftype.Elem()
				}
				if ftype.Kind() != reflect.Struct {
					return nil, errors.New("option ,inline may only be used on a struct or map field")
				}
				// Check for both libyaml.constructor and yaml.Unmarshaler (by method name)
				if reflect.PointerTo(ftype).Implements(constructorType) || hasConstructYAMLMethod(reflect.PointerTo(ftype)) {
					inlineConstructors = append(inlineConstructors, []int{i})
				} else {
					sinfo, err := getStructInfo(ftype)
					if err != nil {
						return nil, err
					}
					for _, index := range sinfo.InlineConstructors {
						inlineConstructors = append(inlineConstructors, append([]int{i}, index...))
					}
					for _, finfo := range sinfo.FieldsList {
						if _, found := fieldsMap[finfo.Key]; found {
							msg := "duplicated key '" + finfo.Key + "' in struct " + st.String()
							return nil, errors.New(msg)
						}
						if finfo.Inline == nil {
							finfo.Inline = []int{i, finfo.Num}
						} else {
							finfo.Inline = append([]int{i}, finfo.Inline...)
						}
						finfo.Id = len(fieldsList)
						fieldsMap[finfo.Key] = finfo
						fieldsList = append(fieldsList, finfo)
					}
				}
			default:
				return nil, errors.New("option ,inline may only be used on a struct or map field")
			}
			continue
		}

		if tag != "" {
			info.Key = tag
		} else {
			info.Key = strings.ToLower(field.Name)
		}

		if _, found = fieldsMap[info.Key]; found {
			msg := "duplicated key '" + info.Key + "' in struct " + st.String()
			return nil, errors.New(msg)
		}

		info.Id = len(fieldsList)
		fieldsList = append(fieldsList, info)
		fieldsMap[info.Key] = info
	}

	sinfo = &structInfo{
		FieldsMap:          fieldsMap,
		FieldsList:         fieldsList,
		InlineMap:          inlineMap,
		InlineConstructors: inlineConstructors,
	}

	fieldMapMutex.Lock()
	structMap[st] = sinfo
	fieldMapMutex.Unlock()
	return sinfo, nil
}

// isZero reports whether v represents the zero value for its type.
// If v implements the IsZeroer interface, IsZero() is called.
// Otherwise, zero is determined by checking type-specific conditions.
// This is used to determine omitempty behavior when marshaling.
func isZero(v reflect.Value) bool {
	kind := v.Kind()
	if z, ok := v.Interface().(IsZeroer); ok {
		if (kind == reflect.Pointer || kind == reflect.Interface) && v.IsNil() {
			return true
		}
		return z.IsZero()
	}
	switch kind {
	case reflect.String:
		return len(v.String()) == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	case reflect.Slice:
		return v.Len() == 0
	case reflect.Map:
		return v.Len() == 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Struct:
		vt := v.Type()
		for i := v.NumField() - 1; i >= 0; i-- {
			if vt.Field(i).PkgPath != "" {
				continue // Private field
			}
			if !isZero(v.Field(i)) {
				return false
			}
		}
		return true
	}
	return false
}

type Constructor struct {
	doc        *Node
	aliases    map[*Node]bool
	TypeErrors []*ConstructError

	stringMapType  reflect.Type
	generalMapType reflect.Type

	KnownFields    bool
	UniqueKeys     bool
	constructCount int
	aliasCount     int
	aliasDepth     int

	mergedFields map[any]bool
}

var (
	nodeType       = reflect.TypeOf(Node{})
	durationType   = reflect.TypeOf(time.Duration(0))
	stringMapType  = reflect.TypeOf(map[string]any{})
	generalMapType = reflect.TypeOf(map[any]any{})
	ifaceType      = generalMapType.Elem()
)

func NewConstructor(opts *Options) *Constructor {
	return &Constructor{
		stringMapType:  stringMapType,
		generalMapType: generalMapType,
		KnownFields:    opts.KnownFields,
		UniqueKeys:     opts.UniqueKeys,
		aliases:        make(map[*Node]bool),
	}
}

// Construct decodes YAML input into the provided output value.
// The out parameter must be a pointer to the value to decode into.
// Returns a [LoadErrors] if type mismatches occur during decoding.
func Construct(in []byte, out any, opts *Options) error {
	d := NewConstructor(opts)
	p := NewComposer(in)
	defer p.Destroy()
	node := p.Parse()
	if node != nil {
		v := reflect.ValueOf(out)
		if v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
		}
		d.Construct(node, v)
	}
	if len(d.TypeErrors) > 0 {
		return &LoadErrors{Errors: d.TypeErrors}
	}
	return nil
}

func (c *Constructor) tagError(n *Node, tag string, out reflect.Value) {
	if n.Tag != "" {
		tag = n.Tag
	}
	value := n.Value
	if tag != seqTag && tag != mapTag {
		if len(value) > 10 {
			value = " `" + value[:7] + "...`"
		} else {
			value = " `" + value + "`"
		}
	}
	c.TypeErrors = append(c.TypeErrors, &ConstructError{
		Err:    fmt.Errorf("cannot construct %s%s into %s", shortTag(tag), value, out.Type()),
		Line:   n.Line,
		Column: n.Column,
	})
}

func (c *Constructor) callConstructor(n *Node, u constructor) (good bool) {
	err := u.UnmarshalYAML(n)
	switch e := err.(type) {
	case nil:
		return true
	case *LoadErrors:
		c.TypeErrors = append(c.TypeErrors, e.Errors...)
		return false
	default:
		c.TypeErrors = append(c.TypeErrors, &ConstructError{
			Err:    err,
			Line:   n.Line,
			Column: n.Column,
		})
		return false
	}
}

func (c *Constructor) callObsoleteConstructor(n *Node, u obsoleteConstructor) (good bool) {
	terrlen := len(c.TypeErrors)
	err := u.UnmarshalYAML(func(v any) (err error) {
		defer handleErr(&err)
		c.Construct(n, reflect.ValueOf(v))
		if len(c.TypeErrors) > terrlen {
			issues := c.TypeErrors[terrlen:]
			c.TypeErrors = c.TypeErrors[:terrlen]
			return &LoadErrors{issues}
		}
		return nil
	})
	switch e := err.(type) {
	case nil:
		return true
	case *LoadErrors:
		c.TypeErrors = append(c.TypeErrors, e.Errors...)
		return false
	default:
		c.TypeErrors = append(c.TypeErrors, &ConstructError{
			Err:    err,
			Line:   n.Line,
			Column: n.Column,
		})
		return false
	}
}

func isTextUnmarshaler(out reflect.Value) bool {
	// Dereference pointers to check the underlying type,
	// similar to how prepare() handles Constructor checks.
	for out.Kind() == reflect.Pointer {
		if out.IsNil() {
			// Create a new instance to check the type
			out = reflect.New(out.Type().Elem()).Elem()
		} else {
			out = out.Elem()
		}
	}
	if out.CanAddr() {
		_, ok := out.Addr().Interface().(encoding.TextUnmarshaler)
		return ok
	}
	return false
}

// prepare initializes and dereferences pointers and calls UnmarshalYAML
// if a value is found to implement it.
// It returns the initialized and dereferenced out value, whether
// construction was already done by UnmarshalYAML, and if so whether
// its types constructed appropriately.
//
// If n holds a null value, prepare returns before doing anything.
func (c *Constructor) prepare(n *Node, out reflect.Value) (newout reflect.Value, constructed, good bool) {
	if n.ShortTag() == nullTag {
		return out, false, false
	}
	again := true
	for again {
		again = false
		if out.Kind() == reflect.Pointer {
			if out.IsNil() {
				out.Set(reflect.New(out.Type().Elem()))
			}
			out = out.Elem()
			again = true
		}
		if out.CanAddr() {
			// Try yaml.Unmarshaler (from root package) first
			if called, good := c.tryCallYAMLConstructor(n, out); called {
				return out, true, good
			}

			outi := out.Addr().Interface()
			// Check for libyaml.constructor
			if u, ok := outi.(constructor); ok {
				good = c.callConstructor(n, u)
				return out, true, good
			}
			if u, ok := outi.(obsoleteConstructor); ok {
				good = c.callObsoleteConstructor(n, u)
				return out, true, good
			}
		}
	}
	return out, false, false
}

func (c *Constructor) fieldByIndex(n *Node, v reflect.Value, index []int) (field reflect.Value) {
	if n.ShortTag() == nullTag {
		return reflect.Value{}
	}
	for _, num := range index {
		for {
			if v.Kind() == reflect.Pointer {
				if v.IsNil() {
					v.Set(reflect.New(v.Type().Elem()))
				}
				v = v.Elem()
				continue
			}
			break
		}
		v = v.Field(num)
	}
	return v
}

const (
	// 400,000 decode operations is ~500kb of dense object declarations, or
	// ~5kb of dense object declarations with 10000% alias expansion
	alias_ratio_range_low = 400000

	// 4,000,000 decode operations is ~5MB of dense object declarations, or
	// ~4.5MB of dense object declarations with 10% alias expansion
	alias_ratio_range_high = 4000000

	// alias_ratio_range is the range over which we scale allowed alias ratios
	alias_ratio_range = float64(alias_ratio_range_high - alias_ratio_range_low)
)

func allowedAliasRatio(constructCount int) float64 {
	switch {
	case constructCount <= alias_ratio_range_low:
		// allow 99% to come from alias expansion for small-to-medium documents
		return 0.99
	case constructCount >= alias_ratio_range_high:
		// allow 10% to come from alias expansion for very large documents
		return 0.10
	default:
		// scale smoothly from 99% down to 10% over the range.
		// this maps to 396,000 - 400,000 allowed alias-driven decodes over the range.
		// 400,000 decode operations is ~100MB of allocations in worst-case scenarios (single-item maps).
		return 0.99 - 0.89*(float64(constructCount-alias_ratio_range_low)/alias_ratio_range)
	}
}

// constructorAdapter is an interface that wraps the root package's Unmarshaler interface.
// This allows the constructor to call constructors that expect *yaml.Node instead of *libyaml.Node.
type constructorAdapter interface {
	CallRootConstructor(n *Node) error
}

// tryCallYAMLConstructor checks if the value has an UnmarshalYAML method that takes
// a *yaml.Node (from the root package) and calls it if found.
// This handles the case where user types implement yaml.Unmarshaler instead of libyaml.constructor.
func (c *Constructor) tryCallYAMLConstructor(n *Node, out reflect.Value) (called bool, good bool) {
	if !out.CanAddr() {
		return false, false
	}

	addr := out.Addr()
	// Check for UnmarshalYAML method
	method := addr.MethodByName("UnmarshalYAML")
	if !method.IsValid() {
		return false, false
	}

	// Check method signature: func(*yaml.Node) error
	mtype := method.Type()
	if mtype.NumIn() != 1 || mtype.NumOut() != 1 {
		return false, false
	}

	// Check if parameter is a pointer to a Node-like struct
	paramType := mtype.In(0)
	if paramType.Kind() != reflect.Ptr {
		return false, false
	}

	elemType := paramType.Elem()
	if elemType.Kind() != reflect.Struct {
		return false, false
	}

	// Check if it's the same underlying type as our Node
	// Both yaml.Node and libyaml.Node have the same structure
	if elemType.Name() != "Node" {
		return false, false
	}

	// Call the method with a converted node
	// Since yaml.Node and libyaml.Node have the same structure,
	// we can convert using unsafe pointer cast
	nodeValue := reflect.NewAt(elemType, reflect.ValueOf(n).UnsafePointer())

	results := method.Call([]reflect.Value{nodeValue})
	err := results[0].Interface()

	if err == nil {
		return true, true
	}

	switch e := err.(type) {
	case *LoadErrors:
		c.TypeErrors = append(c.TypeErrors, e.Errors...)
		return true, false
	default:
		c.TypeErrors = append(c.TypeErrors, &ConstructError{
			Err:    e.(error),
			Line:   n.Line,
			Column: n.Column,
		})
		return true, false
	}
}

func (c *Constructor) Construct(n *Node, out reflect.Value) (good bool) {
	c.constructCount++
	if c.aliasDepth > 0 {
		c.aliasCount++
	}
	if c.aliasCount > 100 && c.constructCount > 1000 && float64(c.aliasCount)/float64(c.constructCount) > allowedAliasRatio(c.constructCount) {
		failf("document contains excessive aliasing")
	}
	if out.Type() == nodeType {
		out.Set(reflect.ValueOf(n).Elem())
		return true
	}

	// When out type implements [encoding.TextUnmarshaler], ensure the node is
	// a scalar. Otherwise, for example, constructing a YAML mapping into
	// a struct having no exported fields, but implementing TextUnmarshaler
	// would silently succeed, but do nothing.
	//
	// Note that this matches the behavior of both encoding/json and encoding/json/v2.
	if n.Kind != ScalarNode && isTextUnmarshaler(out) {
		err := fmt.Errorf("cannot construct %s into %s (TextUnmarshaler)", shortTag(n.Tag), out.Type())
		c.TypeErrors = append(c.TypeErrors, &ConstructError{
			Err:    err,
			Line:   n.Line,
			Column: n.Column,
		})
		return false
	}
	switch n.Kind {
	case DocumentNode:
		return c.document(n, out)
	case AliasNode:
		return c.alias(n, out)
	}
	out, constructed, good := c.prepare(n, out)
	if constructed {
		return good
	}
	switch n.Kind {
	case ScalarNode:
		good = c.scalar(n, out)
	case MappingNode:
		good = c.mapping(n, out)
	case SequenceNode:
		good = c.sequence(n, out)
	case 0:
		if n.IsZero() {
			return c.null(out)
		}
		fallthrough
	default:
		failf("cannot construct node with unknown kind %d", n.Kind)
	}
	return good
}

func (c *Constructor) document(n *Node, out reflect.Value) (good bool) {
	if len(n.Content) == 1 {
		c.doc = n
		c.Construct(n.Content[0], out)
		return true
	}
	return false
}

func (c *Constructor) alias(n *Node, out reflect.Value) (good bool) {
	if c.aliases[n] {
		// TODO this could actually be allowed in some circumstances.
		failf("anchor '%s' value contains itself", n.Value)
	}
	c.aliases[n] = true
	c.aliasDepth++
	good = c.Construct(n.Alias, out)
	c.aliasDepth--
	delete(c.aliases, n)
	return good
}

func (c *Constructor) null(out reflect.Value) bool {
	if out.CanAddr() {
		switch out.Kind() {
		case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice:
			out.Set(reflect.Zero(out.Type()))
			return true
		}
	}
	return false
}

func (c *Constructor) scalar(n *Node, out reflect.Value) bool {
	var tag string
	var resolved any
	if n.indicatedString() {
		tag = strTag
		resolved = n.Value
	} else {
		tag, resolved = resolve(n.Tag, n.Value)
		if tag == binaryTag {
			data, err := base64.StdEncoding.DecodeString(resolved.(string))
			if err != nil {
				failf("!!binary value contains invalid base64 data")
			}
			resolved = string(data)
		}
	}
	if resolved == nil {
		return c.null(out)
	}
	if resolvedv := reflect.ValueOf(resolved); out.Type() == resolvedv.Type() {
		// We've resolved to exactly the type we want, so use that.
		out.Set(resolvedv)
		return true
	}
	// Perhaps we can use the value as a TextUnmarshaler to
	// set its value.
	if out.CanAddr() {
		u, ok := out.Addr().Interface().(encoding.TextUnmarshaler)
		if ok {
			var text []byte
			if tag == binaryTag {
				text = []byte(resolved.(string))
			} else {
				// We let any value be constructed into TextUnmarshaler.
				// That might be more lax than we'd like, but the
				// TextUnmarshaler itself should bowl out any dubious values.
				text = []byte(n.Value)
			}
			err := u.UnmarshalText(text)
			if err != nil {
				c.TypeErrors = append(c.TypeErrors, &ConstructError{
					Err:    err,
					Line:   n.Line,
					Column: n.Column,
				})
				return false
			}
			return true
		}
	}
	switch out.Kind() {
	case reflect.String:
		if tag == binaryTag {
			out.SetString(resolved.(string))
			return true
		}
		out.SetString(n.Value)
		return true
	case reflect.Slice:
		// allow decoding !!binary-tagged value into []byte specifically
		if out.Type().Elem().Kind() == reflect.Uint8 {
			if tag == binaryTag {
				out.SetBytes([]byte(resolved.(string)))
				return true
			}
		}
	case reflect.Interface:
		out.Set(reflect.ValueOf(resolved))
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// This used to work in v2, but it's very unfriendly.
		isDuration := out.Type() == durationType

		switch resolved := resolved.(type) {
		case int:
			if !isDuration && !out.OverflowInt(int64(resolved)) {
				out.SetInt(int64(resolved))
				return true
			} else if isDuration && resolved == 0 {
				out.SetInt(0)
				return true
			}
		case int64:
			if !isDuration && !out.OverflowInt(resolved) {
				out.SetInt(resolved)
				return true
			}
		case uint64:
			if !isDuration && resolved <= math.MaxInt64 {
				intVal := int64(resolved)
				if !out.OverflowInt(intVal) {
					out.SetInt(intVal)
					return true
				}
			}
		case float64:
			if !isDuration && resolved >= math.MinInt64 && resolved <= math.MaxInt64 {
				intVal := int64(resolved)
				// Verify conversion is lossless (handles floating-point precision)
				if float64(intVal) == resolved && !out.OverflowInt(intVal) {
					out.SetInt(intVal)
					return true
				}
			}
		case string:
			if out.Type() == durationType {
				d, err := time.ParseDuration(resolved)
				if err == nil {
					out.SetInt(int64(d))
					return true
				}
			}
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		switch resolved := resolved.(type) {
		case int:
			if resolved >= 0 && !out.OverflowUint(uint64(resolved)) {
				out.SetUint(uint64(resolved))
				return true
			}
		case int64:
			if resolved >= 0 && !out.OverflowUint(uint64(resolved)) {
				out.SetUint(uint64(resolved))
				return true
			}
		case uint64:
			if !out.OverflowUint(resolved) {
				out.SetUint(resolved)
				return true
			}
		case float64:
			if resolved >= 0 && resolved <= math.MaxUint64 {
				uintVal := uint64(resolved)
				// Verify conversion is lossless (handles floating-point precision)
				if float64(uintVal) == resolved && !out.OverflowUint(uintVal) {
					out.SetUint(uintVal)
					return true
				}
			}
		}
	case reflect.Bool:
		switch resolved := resolved.(type) {
		case bool:
			out.SetBool(resolved)
			return true
		case string:
			// This offers some compatibility with the 1.1 spec (https://yaml.org/type/bool.html).
			// It only works if explicitly attempting to construct into a typed bool value.
			switch resolved {
			case "y", "Y", "yes", "Yes", "YES", "on", "On", "ON":
				out.SetBool(true)
				return true
			case "n", "N", "no", "No", "NO", "off", "Off", "OFF":
				out.SetBool(false)
				return true
			}
		}
	case reflect.Float32, reflect.Float64:
		switch resolved := resolved.(type) {
		case int:
			out.SetFloat(float64(resolved))
			return true
		case int64:
			out.SetFloat(float64(resolved))
			return true
		case uint64:
			out.SetFloat(float64(resolved))
			return true
		case float64:
			out.SetFloat(resolved)
			return true
		}
	case reflect.Struct:
		if resolvedv := reflect.ValueOf(resolved); out.Type() == resolvedv.Type() {
			out.Set(resolvedv)
			return true
		}
	case reflect.Pointer:
		panic("yaml internal error: please report the issue")
	}
	c.tagError(n, tag, out)
	return false
}

func settableValueOf(i any) reflect.Value {
	v := reflect.ValueOf(i)
	sv := reflect.New(v.Type()).Elem()
	sv.Set(v)
	return sv
}

func (c *Constructor) sequence(n *Node, out reflect.Value) (good bool) {
	l := len(n.Content)

	var iface reflect.Value
	switch out.Kind() {
	case reflect.Slice:
		out.Set(reflect.MakeSlice(out.Type(), l, l))
	case reflect.Array:
		if l != out.Len() {
			failf("invalid array: want %d elements but got %d", out.Len(), l)
		}
	case reflect.Interface:
		// No type hints. Will have to use a generic sequence.
		iface = out
		out = settableValueOf(make([]any, l))
	default:
		c.tagError(n, seqTag, out)
		return false
	}
	et := out.Type().Elem()

	j := 0
	for i := 0; i < l; i++ {
		e := reflect.New(et).Elem()
		if ok := c.Construct(n.Content[i], e); ok {
			out.Index(j).Set(e)
			j++
		}
	}
	if out.Kind() != reflect.Array {
		out.Set(out.Slice(0, j))
	}
	if iface.IsValid() {
		iface.Set(out)
	}
	return true
}

func (c *Constructor) mapping(n *Node, out reflect.Value) (good bool) {
	l := len(n.Content)
	if c.UniqueKeys {
		nerrs := len(c.TypeErrors)
		for i := 0; i < l; i += 2 {
			ni := n.Content[i]
			for j := i + 2; j < l; j += 2 {
				nj := n.Content[j]
				if ni.Kind == nj.Kind && ni.Value == nj.Value {
					c.TypeErrors = append(c.TypeErrors, &ConstructError{
						Err:    fmt.Errorf("mapping key %#v already defined at line %d", nj.Value, ni.Line),
						Line:   nj.Line,
						Column: nj.Column,
					})
				}
			}
		}
		if len(c.TypeErrors) > nerrs {
			return false
		}
	}
	switch out.Kind() {
	case reflect.Struct:
		return c.mappingStruct(n, out)
	case reflect.Map:
		// okay
	case reflect.Interface:
		iface := out
		if isStringMap(n) {
			out = reflect.MakeMap(c.stringMapType)
		} else {
			out = reflect.MakeMap(c.generalMapType)
		}
		iface.Set(out)
	default:
		c.tagError(n, mapTag, out)
		return false
	}

	outt := out.Type()
	kt := outt.Key()
	et := outt.Elem()

	stringMapType := c.stringMapType
	generalMapType := c.generalMapType
	if outt.Elem() == ifaceType {
		if outt.Key().Kind() == reflect.String {
			c.stringMapType = outt
		} else if outt.Key() == ifaceType {
			c.generalMapType = outt
		}
	}

	mergedFields := c.mergedFields
	c.mergedFields = nil

	var mergeNode *Node

	mapIsNew := false
	if out.IsNil() {
		out.Set(reflect.MakeMap(outt))
		mapIsNew = true
	}
	for i := 0; i < l; i += 2 {
		if isMerge(n.Content[i]) {
			mergeNode = n.Content[i+1]
			continue
		}
		k := reflect.New(kt).Elem()
		if c.Construct(n.Content[i], k) {
			if mergedFields != nil {
				ki := k.Interface()
				if c.getPossiblyUnhashableKey(mergedFields, ki) {
					continue
				}
				c.setPossiblyUnhashableKey(mergedFields, ki, true)
			}
			kkind := k.Kind()
			if kkind == reflect.Interface {
				kkind = k.Elem().Kind()
			}
			if kkind == reflect.Map || kkind == reflect.Slice {
				failf("cannot use '%#v' as a map key; try decoding into yaml.Node", k.Interface())
			}
			e := reflect.New(et).Elem()
			if c.Construct(n.Content[i+1], e) || n.Content[i+1].ShortTag() == nullTag && (mapIsNew || !out.MapIndex(k).IsValid()) {
				out.SetMapIndex(k, e)
			}
		}
	}

	c.mergedFields = mergedFields
	if mergeNode != nil {
		c.merge(n, mergeNode, out)
	}

	c.stringMapType = stringMapType
	c.generalMapType = generalMapType
	return true
}

func isStringMap(n *Node) bool {
	if n.Kind != MappingNode {
		return false
	}
	l := len(n.Content)
	for i := 0; i < l; i += 2 {
		shortTag := n.Content[i].ShortTag()
		if shortTag != strTag && shortTag != mergeTag {
			return false
		}
	}
	return true
}

func (c *Constructor) mappingStruct(n *Node, out reflect.Value) (good bool) {
	sinfo, err := getStructInfo(out.Type())
	if err != nil {
		panic(err)
	}

	var inlineMap reflect.Value
	var elemType reflect.Type
	if sinfo.InlineMap != -1 {
		inlineMap = out.Field(sinfo.InlineMap)
		elemType = inlineMap.Type().Elem()
	}

	for _, index := range sinfo.InlineConstructors {
		field := c.fieldByIndex(n, out, index)
		c.prepare(n, field)
	}

	mergedFields := c.mergedFields
	c.mergedFields = nil
	var mergeNode *Node
	var doneFields []bool
	if c.UniqueKeys {
		doneFields = make([]bool, len(sinfo.FieldsList))
	}
	name := settableValueOf("")
	l := len(n.Content)
	for i := 0; i < l; i += 2 {
		ni := n.Content[i]
		if isMerge(ni) {
			mergeNode = n.Content[i+1]
			continue
		}
		if !c.Construct(ni, name) {
			continue
		}
		sname := name.String()
		if mergedFields != nil {
			if mergedFields[sname] {
				continue
			}
			mergedFields[sname] = true
		}
		if info, ok := sinfo.FieldsMap[sname]; ok {
			if c.UniqueKeys {
				if doneFields[info.Id] {
					c.TypeErrors = append(c.TypeErrors, &ConstructError{
						Err:    fmt.Errorf("field %s already set in type %s", name.String(), out.Type()),
						Line:   ni.Line,
						Column: ni.Column,
					})
					continue
				}
				doneFields[info.Id] = true
			}
			var field reflect.Value
			if info.Inline == nil {
				field = out.Field(info.Num)
			} else {
				field = c.fieldByIndex(n, out, info.Inline)
			}
			c.Construct(n.Content[i+1], field)
		} else if sinfo.InlineMap != -1 {
			if inlineMap.IsNil() {
				inlineMap.Set(reflect.MakeMap(inlineMap.Type()))
			}
			value := reflect.New(elemType).Elem()
			c.Construct(n.Content[i+1], value)
			inlineMap.SetMapIndex(name, value)
		} else if c.KnownFields {
			c.TypeErrors = append(c.TypeErrors, &ConstructError{
				Err:    fmt.Errorf("field %s not found in type %s", name.String(), out.Type()),
				Line:   ni.Line,
				Column: ni.Column,
			})
		}
	}

	c.mergedFields = mergedFields
	if mergeNode != nil {
		c.merge(n, mergeNode, out)
	}
	return true
}

func failWantMap() {
	failf("map merge requires map or sequence of maps as the value")
}

func (c *Constructor) setPossiblyUnhashableKey(m map[any]bool, key any, value bool) {
	defer func() {
		if err := recover(); err != nil {
			failf("%v", err)
		}
	}()
	m[key] = value
}

func (c *Constructor) getPossiblyUnhashableKey(m map[any]bool, key any) bool {
	defer func() {
		if err := recover(); err != nil {
			failf("%v", err)
		}
	}()
	return m[key]
}

func (c *Constructor) merge(parent *Node, merge *Node, out reflect.Value) {
	mergedFields := c.mergedFields
	if mergedFields == nil {
		c.mergedFields = make(map[any]bool)
		for i := 0; i < len(parent.Content); i += 2 {
			k := reflect.New(ifaceType).Elem()
			if c.Construct(parent.Content[i], k) {
				c.setPossiblyUnhashableKey(c.mergedFields, k.Interface(), true)
			}
		}
	}

	switch merge.Kind {
	case MappingNode:
		c.Construct(merge, out)
	case AliasNode:
		if merge.Alias != nil && merge.Alias.Kind != MappingNode {
			failWantMap()
		}
		c.Construct(merge, out)
	case SequenceNode:
		for i := 0; i < len(merge.Content); i++ {
			ni := merge.Content[i]
			if ni.Kind == AliasNode {
				if ni.Alias != nil && ni.Alias.Kind != MappingNode {
					failWantMap()
				}
			} else if ni.Kind != MappingNode {
				failWantMap()
			}
			c.Construct(ni, out)
		}
	default:
		failWantMap()
	}

	c.mergedFields = mergedFields
}

func isMerge(n *Node) bool {
	return n.Kind == ScalarNode && shortTag(n.Tag) == mergeTag
}
