// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/low/base"

	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"

	"github.com/pb33f/libopenapi/datamodel/low"
	"go.yaml.in/yaml/v4"
)

const (
	HashPh    = "%x"
	EMPTY_STR = ""
)

var changeMutex sync.Mutex

// SetReferenceIfExists checks if a low-level value has a reference and sets it on the change object
// if the change object implements the ChangeIsReferenced interface.
func SetReferenceIfExists[T any](value *low.ValueReference[T], changeObj any) {
	if value != nil && value.IsReference() {
		if refChange, ok := changeObj.(ChangeIsReferenced); ok {
			refChange.SetChangeReference(value.GetReference())
		}
	}
}

// PreserveParameterReference checks if a parameter is a reference and preserves it on the changes object.
// This eliminates duplicate reference preservation logic in operation.go and path_item.go.
func PreserveParameterReference[T any](lRefs, rRefs map[string]*low.ValueReference[T], name string, changes ChangeIsReferenced) {
	if lRef := lRefs[name]; lRef != nil && lRef.IsReference() {
		SetReferenceIfExists(lRef, changes)
	} else if rRef := rRefs[name]; rRef != nil && rRef.IsReference() {
		SetReferenceIfExists(rRef, changes)
	}
}

func checkLocation(ctx *ChangeContext, hs base.HasIndex) bool {
	if !reflect.ValueOf(hs).IsNil() {
		idx := hs.GetIndex()
		if idx == nil {
			return false
		}
		if idx.GetRolodex() != nil {
			r := idx.GetRolodex()
			rIdx := r.GetRootIndex()
			if rIdx.GetSpecAbsolutePath() != idx.GetSpecAbsolutePath() {
				ctx.DocumentLocation = idx.GetSpecAbsolutePath()
				return true
			}
		}
	}
	return false
}

// CreateChange is a generic function that will create a Change of type T, populate all properties if set and then
// add a pointer to Change[T] in the slice of Change pointers provided
func CreateChange(changes *[]*Change, changeType int, property string, leftValueNode, rightValueNode *yaml.Node,
	breaking bool, originalObject, newObject any,
) *[]*Change {
	// create a new context for the left and right nodes.
	ctx := CreateContext(leftValueNode, rightValueNode)
	c := &Change{
		Context:    ctx,
		ChangeType: changeType,
		Property:   property,
		Breaking:   breaking,
	}

	// lets find out if the objects are local to the root, or if it's come from another document in the tree.
	if originalObject != nil {
		if hs, ok := originalObject.(base.HasIndex); ok {
			checkLocation(ctx, hs)
		}
	}
	if newObject != nil {
		if hs, ok := newObject.(base.HasIndex); ok {
			checkLocation(ctx, hs)
		}
	}

	// if the left is not nil, we have an original value
	if leftValueNode != nil && leftValueNode.Value != EMPTY_STR {
		c.Original = leftValueNode.Value
	}
	// if the right is not nil, then we have a new value
	if rightValueNode != nil && rightValueNode.Value != EMPTY_STR {
		c.New = rightValueNode.Value
	}

	// If node is nil but object is a string, use the object value as fallback
	// This handles cases where the value is provided as the object parameter (e.g., security requirements)
	if leftValueNode == nil && c.Original == "" {
		if str, ok := originalObject.(string); ok {
			c.Original = str
		}
	}
	if rightValueNode == nil && c.New == "" {
		if str, ok := newObject.(string); ok {
			c.New = str
		}
	}

	// original and new objects
	c.OriginalObject = originalObject
	c.NewObject = newObject

	// add the change to supplied changes slice
	changeMutex.Lock()
	*changes = append(*changes, c)
	changeMutex.Unlock()
	return changes
}

// CreateChangeWithEncoding is like CreateChange but also populates the encoded fields for complex values.
// use this ONLY for extensions or other cases where complex YAML structures need to be serialized.
// the encoded values are serialized to YAML format.
func CreateChangeWithEncoding(changes *[]*Change, changeType int, property string, leftValueNode, rightValueNode *yaml.Node,
	breaking bool, originalObject, newObject any,
) *[]*Change {
	CreateChange(changes, changeType, property, leftValueNode, rightValueNode, breaking, originalObject, newObject)

	c := (*changes)[len(*changes)-1]

	// serialize complex values to YAML for extension rendering (avoid inflating memory for scalar values)
	if leftValueNode != nil && (utils.IsNodeArray(leftValueNode) || utils.IsNodeMap(leftValueNode)) {
		if encoded, err := yaml.Marshal(leftValueNode); err == nil {
			c.OriginalEncoded = string(encoded)
		}
	}
	if rightValueNode != nil && (utils.IsNodeArray(rightValueNode) || utils.IsNodeMap(rightValueNode)) {
		if encoded, err := yaml.Marshal(rightValueNode); err == nil {
			c.NewEncoded = string(encoded)
		}
	}

	return changes
}

// CreateContext will return a pointer to a ChangeContext containing the original and new line and column numbers
// of the left and right value nodes.
func CreateContext(l, r *yaml.Node) *ChangeContext {
	ctx := new(ChangeContext)
	if l != nil {
		ctx.OriginalLine = &l.Line
		ctx.OriginalColumn = &l.Column
	}
	if r != nil {
		ctx.NewLine = &r.Line
		ctx.NewColumn = &r.Column
	}
	return ctx
}

func FlattenLowLevelOrderedMap[T any](
	lowMap *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
) map[string]*low.ValueReference[T] {
	flat := make(map[string]*low.ValueReference[T])

	for k, l := range lowMap.FromOldest() {
		flat[k.Value] = &l
	}
	return flat
}

// CountBreakingChanges counts the number of changes in a slice that are breaking
func CountBreakingChanges(changes []*Change) int {
	b := 0
	for i := range changes {
		if changes[i].Breaking {
			b++
		}
	}
	return b
}

// checkForObjectAdditionOrRemovalInternal is the internal implementation that handles both encoding modes.
func checkForObjectAdditionOrRemovalInternal[T any](l, r map[string]*low.ValueReference[T], label string, changes *[]*Change,
	breakingAdd, breakingRemove bool, withEncoding bool,
) {
	createFn := CreateChange
	if withEncoding {
		createFn = CreateChangeWithEncoding
	}
	var left, right T
	if CheckSpecificObjectRemoved(l, r, label) {
		left = l[label].GetValue()
		createFn(changes, ObjectRemoved, label, l[label].GetValueNode(), nil,
			breakingRemove, left, nil)
	}
	if CheckSpecificObjectAdded(l, r, label) {
		right = r[label].GetValue()
		createFn(changes, ObjectAdded, label, nil, r[label].GetValueNode(),
			breakingAdd, nil, right)
	}
}

// CheckForObjectAdditionOrRemoval will check for the addition or removal of an object from left and right maps.
// The label is the key to look for in the left and right maps.
//
// To determine this a breaking change for an addition then set breakingAdd to true (however I can't think of many
// scenarios that adding things should break anything). Removals are generally breaking, except for non contract
// properties like descriptions, summaries and other non-binding values, so a breakingRemove value can be tuned for
// these circumstances.
func CheckForObjectAdditionOrRemoval[T any](l, r map[string]*low.ValueReference[T], label string, changes *[]*Change,
	breakingAdd, breakingRemove bool,
) {
	checkForObjectAdditionOrRemovalInternal(l, r, label, changes, breakingAdd, breakingRemove, false)
}

// CheckForObjectAdditionOrRemovalWithEncoding is like CheckForObjectAdditionOrRemoval but populates encoded fields.
// Use this for extensions where complex values need to be serialized to YAML.
func CheckForObjectAdditionOrRemovalWithEncoding[T any](l, r map[string]*low.ValueReference[T], label string, changes *[]*Change,
	breakingAdd, breakingRemove bool,
) {
	checkForObjectAdditionOrRemovalInternal(l, r, label, changes, breakingAdd, breakingRemove, true)
}

// CheckSpecificObjectRemoved returns true if a specific value is not in both maps.
func CheckSpecificObjectRemoved[T any](l, r map[string]*T, label string) bool {
	return l[label] != nil && r[label] == nil
}

// CheckSpecificObjectAdded returns true if a specific value is not in both maps.
func CheckSpecificObjectAdded[T any](l, r map[string]*T, label string) bool {
	return l[label] == nil && r[label] != nil
}

// CheckProperties will iterate through a slice of PropertyCheck pointers of type T. The method is a convenience method
// for running checks on the following methods in order:
//
//	CheckPropertyAdditionOrRemoval
//	CheckForModification
//
// When PropertyCheck has Component set, the configurable breaking rules system is used
// to look up the correct breaking value for each change type (added, modified, removed).
func CheckProperties(properties []*PropertyCheck) {
	checkPropertiesInternal(properties, false)
}

// checkPropertiesInternal is the shared implementation for CheckProperties and CheckPropertiesWithEncoding.
// The withEncoding parameter controls whether to use encoding-aware functions for complex YAML values.
func checkPropertiesInternal(properties []*PropertyCheck, withEncoding bool) {
	// cache config once outside the loop for performance (avoids repeated mutex operations)
	config := GetActiveBreakingRulesConfig()

	for _, n := range properties {
		var breakingAdded, breakingModified, breakingRemoved bool

		if n.Component != "" {
			// use configurable breaking rules via cached config if rule exists
			if rule := config.GetRule(n.Component, n.Property); rule != nil {
				// extract breaking values directly from rule (avoids 3 redundant lookups)
				breakingAdded = rule.Added != nil && *rule.Added
				breakingModified = rule.Modified != nil && *rule.Modified
				breakingRemoved = rule.Removed != nil && *rule.Removed
			} else {
				// no rule found - fallback to legacy Breaking field
				breakingAdded = n.Breaking
				breakingModified = n.Breaking
				breakingRemoved = n.Breaking
			}
		} else {
			// no component set - fallback to legacy Breaking field
			breakingAdded = n.Breaking
			breakingModified = n.Breaking
			breakingRemoved = n.Breaking
		}

		// run the checks with the determined breaking values
		if withEncoding {
			checkForRemovalInternal(n.LeftNode, n.RightNode, n.Label, n.Changes, breakingRemoved, n.Original, n.New, true)
			checkForAdditionInternal(n.LeftNode, n.RightNode, n.Label, n.Changes, breakingAdded, n.Original, n.New, true)
			checkForModificationInternal(n.LeftNode, n.RightNode, n.Label, n.Changes, breakingModified, n.Original, n.New, true)
		} else {
			checkForRemovalInternal(n.LeftNode, n.RightNode, n.Label, n.Changes, breakingRemoved, n.Original, n.New, false)
			checkForAdditionInternal(n.LeftNode, n.RightNode, n.Label, n.Changes, breakingAdded, n.Original, n.New, false)
			checkForModificationInternal(n.LeftNode, n.RightNode, n.Label, n.Changes, breakingModified, n.Original, n.New, false)
		}
	}
}

// CheckPropertiesWithEncoding is like CheckProperties but uses CreateChangeWithEncoding for complex values.
// Use this for extensions where YAML serialization is needed.
func CheckPropertiesWithEncoding(properties []*PropertyCheck) {
	checkPropertiesInternal(properties, true)
}

// CheckPropertyAdditionOrRemovalWithEncoding checks for additions and removals with encoding.
func CheckPropertyAdditionOrRemovalWithEncoding[T any](l, r *yaml.Node,
	label string, changes *[]*Change, breaking bool, orig, new T,
) {
	checkForRemovalInternal(l, r, label, changes, breaking, orig, new, true)
	checkForAdditionInternal(l, r, label, changes, breaking, orig, new, true)
}

// CheckForRemovalWithEncoding checks for removals with YAML encoding.
func CheckForRemovalWithEncoding[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T) {
	checkForRemovalInternal(l, r, label, changes, breaking, orig, new, true)
}

// CheckForAdditionWithEncoding checks for additions with YAML encoding.
func CheckForAdditionWithEncoding[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T) {
	checkForAdditionInternal(l, r, label, changes, breaking, orig, new, true)
}

// CheckForModificationWithEncoding checks for modifications with YAML encoding.
func CheckForModificationWithEncoding[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T) {
	checkForModificationInternal(l, r, label, changes, breaking, orig, new, true)
}

// CheckPropertyAdditionOrRemoval will run both CheckForRemoval (first) and CheckForAddition (second)
func CheckPropertyAdditionOrRemoval[T any](l, r *yaml.Node,
	label string, changes *[]*Change, breaking bool, orig, new T,
) {
	CheckForRemoval[T](l, r, label, changes, breaking, orig, new)
	CheckForAddition[T](l, r, label, changes, breaking, orig, new)
}

// checkForRemovalInternal is the internal implementation for removal checks with configurable encoding.
func checkForRemovalInternal[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T, withEncoding bool) {
	createFn := CreateChange
	if withEncoding {
		createFn = CreateChangeWithEncoding
	}
	if l != nil && l.Value != EMPTY_STR && (r == nil || r.Value == EMPTY_STR && !utils.IsNodeArray(r) && !utils.IsNodeMap(r)) {
		createFn(changes, PropertyRemoved, label, l, r, breaking, orig, new)
		return
	}
	if l != nil && r == nil {
		createFn(changes, PropertyRemoved, label, l, nil, breaking, orig, nil)
	}
}

// CheckForRemoval will check left and right yaml.Node instances for changes. Anything that is found missing on the
// right, but present on the left, is considered a removal. A new Change[T] will be created with the type
//
//	PropertyRemoved
//
// The Change is then added to the slice of []Change[T] instances provided as a pointer.
func CheckForRemoval[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T) {
	checkForRemovalInternal(l, r, label, changes, breaking, orig, new, false)
}

// checkForAdditionInternal is the internal implementation for addition checks with configurable encoding.
func checkForAdditionInternal[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T, withEncoding bool) {
	createFn := CreateChange
	if withEncoding {
		createFn = CreateChangeWithEncoding
	}
	// left doesn't exist if: nil OR (empty scalar AND not a map/array) OR (empty map/array)
	leftDoesNotExist := l == nil ||
		(l.Value == EMPTY_STR && !utils.IsNodeMap(l) && !utils.IsNodeArray(l)) ||
		((utils.IsNodeMap(l) || utils.IsNodeArray(l)) && len(l.Content) == 0)
	// right exists if: not nil AND (has value OR is array OR is map)
	rightExists := r != nil && (r.Value != EMPTY_STR || utils.IsNodeArray(r) || utils.IsNodeMap(r))

	if leftDoesNotExist && rightExists {
		createFn(changes, PropertyAdded, label, l, r, breaking, orig, new)
	}
}

// CheckForAddition will check left and right yaml.Node instances for changes. Anything that is found missing on the
// left, but present on the right, is considered an addition. A new Change[T] will be created with the type
//
//	PropertyAdded
//
// The Change is then added to the slice of []Change[T] instances provided as a pointer.
func CheckForAddition[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T) {
	checkForAdditionInternal(l, r, label, changes, breaking, orig, new, false)
}

// checkForModificationInternal is the internal implementation for modification checks with configurable encoding.
func checkForModificationInternal[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T, withEncoding bool) {
	createFn := CreateChange
	if withEncoding {
		createFn = CreateChangeWithEncoding
	}
	if l != nil && l.Value != EMPTY_STR && r != nil && r.Value != EMPTY_STR && (r.Value != l.Value || r.Tag != l.Tag) {
		createFn(changes, Modified, label, l, r, breaking, orig, new)
		return
	}
	if l != nil && utils.IsNodeArray(l) && r != nil && !utils.IsNodeArray(r) {
		createFn(changes, Modified, label, l, r, breaking, orig, new)
		return
	}
	if l != nil && !utils.IsNodeArray(l) && r != nil && utils.IsNodeArray(r) {
		createFn(changes, Modified, label, l, r, breaking, orig, new)
		return
	}
	if l != nil && utils.IsNodeMap(l) && r != nil && !utils.IsNodeMap(r) {
		createFn(changes, Modified, label, l, r, breaking, orig, new)
		return
	}
	if l != nil && !utils.IsNodeMap(l) && r != nil && utils.IsNodeMap(r) {
		createFn(changes, Modified, label, l, r, breaking, orig, new)
		return
	}
	if l != nil && utils.IsNodeArray(l) && r != nil && utils.IsNodeArray(r) {
		if len(l.Content) != len(r.Content) {
			createFn(changes, Modified, label, l, r, breaking, orig, new)
			return
		}

		// Compare the YAML node trees directly without marshaling
		if !low.CompareYAMLNodes(l, r) {
			createFn(changes, Modified, label, l, r, breaking, orig, new)
		}
		return
	}
	if l != nil && utils.IsNodeMap(l) && r != nil && utils.IsNodeMap(r) {
		// Compare the YAML node trees directly without marshaling
		if !low.CompareYAMLNodes(l, r) {
			createFn(changes, Modified, label, l, r, breaking, orig, new)
		}
		return
	}
}

// CheckForModification will check left and right yaml.Node instances for changes. Anything that is found in both
// sides, but vary in value is considered a modification.
//
// If there is a change in value the function adds a change type of Modified.
//
// The Change is then added to the slice of []Change[T] instances provided as a pointer.
func CheckForModification[T any](l, r *yaml.Node, label string, changes *[]*Change, breaking bool, orig, new T) {
	checkForModificationInternal(l, r, label, changes, breaking, orig, new, false)
}

// CheckMapForChanges checks a left and right low level map for any additions, subtractions or modifications to
// values. The compareFunc argument should reference the correct comparison function for the generic type.
// Uses original hardcoded breaking behavior (removals breaking, additions non-breaking).
func CheckMapForChanges[T any, R any](expLeft, expRight *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
	changes *[]*Change, label string, compareFunc func(l, r T) R,
) map[string]R {
	return checkMapForChangesInternal(expLeft, expRight, changes, label, compareFunc, true, false, true)
}

// CheckMapForChangesWithRules checks a left and right low level map for any additions, subtractions or modifications
// to values, using the configurable breaking rules system for the specified component and property.
func CheckMapForChangesWithRules[T any, R any](expLeft, expRight *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
	changes *[]*Change, label string, compareFunc func(l, r T) R, component, property string,
) map[string]R {
	return checkMapForChangesInternal(expLeft, expRight, changes, label, compareFunc, true,
		BreakingAdded(component, property), BreakingRemoved(component, property))
}

// CheckMapForAdditionRemoval checks a left and right low level map for any additions or subtractions, but not modifications
func CheckMapForAdditionRemoval[T any](expLeft, expRight *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
	changes *[]*Change, label string,
) any {
	doNothing := func(l, r T) any {
		return nil
	}
	// adding purely to make sure code is called for coverage.
	var l, r T
	doNothing(l, r)
	return checkMapForChangesInternal(expLeft, expRight, changes, label, doNothing, false, false, true)
}

// CheckMapForChangesWithComp checks a left and right low level map for any additions, subtractions or modifications to
// values. The compareFunc argument should reference the correct comparison function for the generic type. The compare
// bit determines if the comparison should be run or not.
// Deprecated: Use checkMapForChangesInternal with explicit breaking parameters instead.
func CheckMapForChangesWithComp[T any, R any](expLeft, expRight *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
	changes *[]*Change, label string, compareFunc func(l, r T) R, compare bool,
) map[string]R {
	return checkMapForChangesInternal(expLeft, expRight, changes, label, compareFunc, compare, false, true)
}

// CheckMapForChangesWithNilSupport checks a left and right low level map for any additions, subtractions or modifications.
// Unlike CheckMapForChanges, this function calls compareFunc for added/removed items by passing nil for the missing side.
// The compareFunc MUST handle nil inputs gracefully (return appropriate changes for added/removed cases).
// This allows the returned map to include entries for added/removed items, enabling proper tree rendering.
func CheckMapForChangesWithNilSupport[T any, R any](expLeft, expRight *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
	changes *[]*Change, label string, compareFunc func(l, r T) R,
) map[string]R {
	return checkMapForChangesWithNilSupportInternal(expLeft, expRight, changes, label, compareFunc, false, true)
}

// checkMapForChangesWithNilSupportInternal is the core implementation that calls compareFunc with nil for added/removed items.
func checkMapForChangesWithNilSupportInternal[T any, R any](expLeft, expRight *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
	changes *[]*Change, label string, compareFunc func(l, r T) R,
	breakingAdded, breakingRemoved bool,
) map[string]R {
	var chLock sync.Mutex

	lHashes := make(map[string]string)
	rHashes := make(map[string]string)
	lValues := make(map[string]low.ValueReference[T])
	rValues := make(map[string]low.ValueReference[T])

	if expLeft != nil {
		for k, v := range expLeft.FromOldest() {
			lHashes[k.Value] = low.GenerateHashString(v.Value)
			lValues[k.Value] = v
		}
	}

	if expRight != nil {
		for k, v := range expRight.FromOldest() {
			rHashes[k.Value] = low.GenerateHashString(v.Value)
			rValues[k.Value] = v
		}
	}

	expChanges := make(map[string]R)

	checkLeft := func(k string, doneChan chan struct{}, f, g map[string]string, p, h map[string]low.ValueReference[T]) {
		rhash := g[k]
		if rhash == EMPTY_STR {
			// Item was removed - call compareFunc with nil/zero right side
			chLock.Lock()
			var zero T
			ch := compareFunc(p[k].Value, zero)
			if !reflect.ValueOf(&ch).Elem().IsZero() {
				expChanges[k] = ch
				var cr any = ch
				pVal := p[k]
				SetReferenceIfExists(&pVal, cr)
			}
			chLock.Unlock()
			doneChan <- struct{}{}
			return
		}
		if f[k] == g[k] {
			doneChan <- struct{}{}
			return
		}
		// Item was modified
		chLock.Lock()
		ch := compareFunc(p[k].Value, h[k].Value)
		if !reflect.ValueOf(&ch).Elem().IsZero() {
			expChanges[k] = ch
			var cr any = ch
			pVal := p[k]
			SetReferenceIfExists(&pVal, cr)
		}
		chLock.Unlock()
		doneChan <- struct{}{}
	}

	checkRight := func(k string, doneChan chan struct{}, f map[string]string, p map[string]low.ValueReference[T]) {
		lhash := f[k]
		if lhash == EMPTY_STR {
			// Item was added - call compareFunc with nil/zero left side
			chLock.Lock()
			var zero T
			ch := compareFunc(zero, p[k].Value)
			if !reflect.ValueOf(&ch).Elem().IsZero() {
				expChanges[k] = ch
				var cr any = ch
				pVal := p[k]
				SetReferenceIfExists(&pVal, cr)
			}
			chLock.Unlock()
		}
		doneChan <- struct{}{}
	}

	doneChan := make(chan struct{})
	count := 0

	for k := range lHashes {
		count++
		go checkLeft(k, doneChan, lHashes, rHashes, lValues, rValues)
	}

	for k := range rHashes {
		count++
		go checkRight(k, doneChan, lHashes, rValues)
	}

	completed := 0
	for completed < count {
		<-doneChan
		completed++
	}
	return expChanges
}

// checkMapForChangesInternal is the core implementation that checks a left and right low level map for any
// additions, subtractions or modifications to values. The breakingAdded and breakingRemoved parameters control
// whether additions and removals are marked as breaking changes.
func checkMapForChangesInternal[T any, R any](expLeft, expRight *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
	changes *[]*Change, label string, compareFunc func(l, r T) R, compare bool,
	breakingAdded, breakingRemoved bool,
) map[string]R {
	var chLock sync.Mutex

	lHashes := make(map[string]string)
	rHashes := make(map[string]string)
	lValues := make(map[string]low.ValueReference[T])
	rValues := make(map[string]low.ValueReference[T])

	if expLeft != nil {
		for k, v := range expLeft.FromOldest() {
			lHashes[k.Value] = low.GenerateHashString(v.Value)
			lValues[k.Value] = v
		}
	}

	if expRight != nil {
		for k, v := range expRight.FromOldest() {
			rHashes[k.Value] = low.GenerateHashString(v.Value)
			rValues[k.Value] = v
		}
	}

	expChanges := make(map[string]R)

	checkLeft := func(k string, doneChan chan struct{}, f, g map[string]string, p, h map[string]low.ValueReference[T]) {
		rhash := g[k]
		if rhash == EMPTY_STR {
			chLock.Lock()
			if p[k].GetValueNode().Value == EMPTY_STR {
				p[k].GetValueNode().Value = k
			}
			CreateChange(changes, ObjectRemoved, label,
				p[k].GetValueNode(), nil, breakingRemoved,
				p[k].GetValue(), nil)
			chLock.Unlock()
			doneChan <- struct{}{}
			return
		}
		if f[k] == g[k] {
			doneChan <- struct{}{}
			return
		}
		if compare {
			chLock.Lock()
			ch := compareFunc(p[k].Value, h[k].Value)
			// incorrect map results were being generated causing panics.
			// https://github.com/pb33f/libopenapi/issues/61
			if !reflect.ValueOf(&ch).Elem().IsZero() {
				expChanges[k] = ch
				var cr any = ch
				pVal := p[k]
				SetReferenceIfExists(&pVal, cr)
			}
			chLock.Unlock()
		}
		doneChan <- struct{}{}
	}

	checkRight := func(k string, doneChan chan struct{}, f map[string]string, p map[string]low.ValueReference[T]) {
		lhash := f[k]
		if lhash == EMPTY_STR {
			chLock.Lock()
			if p[k].GetValueNode().Value == EMPTY_STR {
				p[k].GetValueNode().Value = k
			}
			CreateChange(changes, ObjectAdded, label,
				nil, p[k].GetValueNode(), breakingAdded,
				nil, p[k].GetValue())
			chLock.Unlock()
		}
		doneChan <- struct{}{}
	}

	doneChan := make(chan struct{})
	count := 0

	for k := range lHashes {
		count++
		go checkLeft(k, doneChan, lHashes, rHashes, lValues, rValues)
	}

	for k := range rHashes {
		count++
		go checkRight(k, doneChan, lHashes, rValues)
	}

	completed := 0
	for completed < count {
		<-doneChan
		completed++
	}
	return expChanges
}

// ExtractStringValueSliceChanges will compare two low level string slices for changes.
// The breaking parameter is deprecated - use ExtractStringValueSliceChangesWithRules instead.
func ExtractStringValueSliceChanges(lParam, rParam []low.ValueReference[string],
	changes *[]*Change, label string, breaking bool,
) {
	lKeys := make([]string, len(lParam))
	rKeys := make([]string, len(rParam))
	lValues := make(map[string]low.ValueReference[string])
	rValues := make(map[string]low.ValueReference[string])
	for i := range lParam {
		lKeys[i] = strings.ToLower(lParam[i].Value)
		lValues[lKeys[i]] = lParam[i]
	}
	for i := range rParam {
		rKeys[i] = strings.ToLower(rParam[i].Value)
		rValues[rKeys[i]] = rParam[i]
	}
	for i := range lValues {
		if _, ok := rValues[i]; !ok {
			CreateChange(changes, PropertyRemoved, label,
				lValues[i].ValueNode,
				nil,
				breaking,
				lValues[i].Value,
				nil)
		}
	}
	for i := range rValues {
		if _, ok := lValues[i]; !ok {
			CreateChange(changes, PropertyAdded, label,
				nil,
				rValues[i].ValueNode,
				false,
				nil,
				rValues[i].Value)
		}
	}
}

// ExtractStringValueSliceChangesWithRules compares two low level string slices for changes,
// using the configurable breaking rules system to determine breaking status.
func ExtractStringValueSliceChangesWithRules(lParam, rParam []low.ValueReference[string],
	changes *[]*Change, label string, component, property string,
) {
	lKeys := make([]string, len(lParam))
	rKeys := make([]string, len(rParam))
	lValues := make(map[string]low.ValueReference[string])
	rValues := make(map[string]low.ValueReference[string])
	for i := range lParam {
		lKeys[i] = strings.ToLower(lParam[i].Value)
		lValues[lKeys[i]] = lParam[i]
	}
	for i := range rParam {
		rKeys[i] = strings.ToLower(rParam[i].Value)
		rValues[rKeys[i]] = rParam[i]
	}
	for i := range lValues {
		if _, ok := rValues[i]; !ok {
			CreateChange(changes, PropertyRemoved, label,
				lValues[i].ValueNode,
				nil,
				BreakingRemoved(component, property),
				lValues[i].Value,
				nil)
		}
	}
	for i := range rValues {
		if _, ok := lValues[i]; !ok {
			CreateChange(changes, PropertyAdded, label,
				nil,
				rValues[i].ValueNode,
				BreakingAdded(component, property),
				nil,
				rValues[i].Value)
		}
	}
}

func toString(v any) string {
	if y, ok := v.(*yaml.Node); ok {
		copy := *y
		_ = copy.Encode(&copy)
		return fmt.Sprint(copy)
	}

	return fmt.Sprint(v)
}

// ExtractRawValueSliceChanges will compare two low level interface{} slices for changes.
func ExtractRawValueSliceChanges[T any](lParam, rParam []low.ValueReference[T],
	changes *[]*Change, label string, breaking bool,
) {
	lKeys := make([]string, len(lParam))
	rKeys := make([]string, len(rParam))
	lValues := make(map[string]low.ValueReference[T])
	rValues := make(map[string]low.ValueReference[T])
	for i := range lParam {
		lKeys[i] = strings.ToLower(toString(lParam[i].Value))
		lValues[lKeys[i]] = lParam[i]
	}
	for i := range rParam {
		rKeys[i] = strings.ToLower(toString(rParam[i].Value))
		rValues[rKeys[i]] = rParam[i]
	}
	for i := range lValues {
		if _, ok := rValues[i]; !ok {
			CreateChange(changes, PropertyRemoved, label,
				lValues[i].ValueNode,
				nil,
				breaking,
				lValues[i].Value,
				nil)
		}
	}
	for i := range rValues {
		if _, ok := lValues[i]; !ok {
			CreateChange(changes, PropertyAdded, label,
				nil,
				rValues[i].ValueNode,
				false,
				nil,
				rValues[i].Value)
		}
	}
}
