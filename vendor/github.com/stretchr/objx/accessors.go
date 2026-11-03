package objx

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

const (
	// PathSeparator is the character used to separate the elements
	// of the keypath.
	//
	// For example, `location.address.city`
	PathSeparator string = "."

	// arrayAccessRegexString is the regex used to extract the array number
	// from the access path
	arrayAccessRegexString = `^(.+)\[([0-9]+)\]$`

	// mapAccessRegexString is the regex used to extract the map key
	// from the access path
	mapAccessRegexString = `^([^\[]*)\[([^\]]+)\](.*)$`
)

// arrayAccessRegex is the compiled arrayAccessRegexString
var arrayAccessRegex = regexp.MustCompile(arrayAccessRegexString)

// mapAccessRegex is the compiled mapAccessRegexString
var mapAccessRegex = regexp.MustCompile(mapAccessRegexString)

// Get gets the value using the specified selector and
// returns it inside a new Obj object.
//
// If it cannot find the value, Get will return a nil
// value inside an instance of Obj.
//
// Get can only operate directly on map[string]any and []interface.
//
// # Example
//
// To access the title of the third chapter of the second book, do:
//
//	o.Get("books[1].chapters[2].title")
func (m Map) Get(selector string) *Value {
	rawObj, _ := access(m, selector, nil, actionGet)
	return &Value{data: rawObj}
}

// Set sets the value using the specified selector and
// returns the object on which Set was called.
//
// Set can only operate directly on map[string]any and []interface
//
// # Example
//
// To set the title of the third chapter of the second book, do:
//
//	o.Set("books[1].chapters[2].title","Time to Go")
func (m Map) Set(selector string, value any) Map {
	access(m, selector, value, actionSet)
	return m
}

// Delete removes map keys or slice elements using each selector in order and
// returns m. It traverses Map, map[string]any, and slices using the same dot and
// bracket notation as Get. Missing paths and out-of-range indexes are ignored.
// Calling Delete with no selectors or on a nil Map is a no-op.
//
// Map keys are deleted in place. Removing a slice element shifts later elements
// down by one, so subsequent selectors use the updated indexes. Slice deletions
// preserve slice types and copy affected slices, including enclosing slices,
// without changing their existing backing arrays. The new slice is stored in
// the nearest enclosing map, which, like every map on the path, is modified in
// place.
//
// # Example
//
// To delete a chapter's title and the draft flag, do:
//
//	o.Delete("books[1].chapters[2].title", "draft")
func (m Map) Delete(selectors ...string) Map {
	for _, selector := range selectors {
		access(m, selector, nil, actionDelete)
	}
	return m
}

// getIndex returns the index, which is hold in s by two branches.
// It also returns s without the index part, e.g. name[1] will return (1, name).
// If no index is found, -1 is returned
func getIndex(s string) (int, string) {
	arrayMatches := arrayAccessRegex.FindStringSubmatch(s)
	if len(arrayMatches) > 0 {
		// Get the key into the map
		selector := arrayMatches[1]
		// Get the index into the array at the key
		// We know this can't fail because arrayMatches[2] is an int for sure
		index, _ := strconv.Atoi(arrayMatches[2])
		return index, selector
	}
	return -1, s
}

// getKey returns the key which is held in s by two brackets.
// It also returns the next selector.
func getKey(s string) (string, string) {
	selSegs := strings.SplitN(s, PathSeparator, 2)
	thisSel := selSegs[0]
	nextSel := ""

	if len(selSegs) > 1 {
		nextSel = selSegs[1]
	}

	mapMatches := mapAccessRegex.FindStringSubmatch(s)
	if len(mapMatches) > 0 {
		if _, err := strconv.Atoi(mapMatches[2]); err != nil {
			thisSel = mapMatches[1]
			nextSel = "[" + mapMatches[2] + "]" + mapMatches[3]

			if thisSel == "" {
				thisSel = mapMatches[2]
				nextSel = mapMatches[3]
			}

			nextSel = strings.TrimPrefix(nextSel, ".")
		}
	}

	return thisSel, nextSel
}

type accessAction int

const (
	actionGet accessAction = iota
	actionSet
	actionDelete
)

// access accesses the object using the selector and performs the
// appropriate action.
func access(current any, selector string, value any, action accessAction) (any, bool) {
	thisSel, nextSel := getKey(selector)

	indexes := []int{}
	for strings.Contains(thisSel, "[") {
		prevSel := thisSel
		var index int
		index, thisSel = getIndex(thisSel)
		indexes = append(indexes, index)
		if prevSel == thisSel {
			break
		}
	}

	if curMap, ok := current.(Map); ok {
		current = map[string]any(curMap)
	}
	// get the object in question
	switch current.(type) {
	case map[string]any:
		curMSI := current.(map[string]any)
		if nextSel == "" {
			switch {
			case action == actionSet && len(indexes) == 0:
				curMSI[thisSel] = value
				return nil, true
			case action == actionDelete && len(indexes) == 0:
				delete(curMSI, thisSel)
				return nil, true
			case action == actionDelete:
				updated, ok := deleteSliceIndex(curMSI[thisSel], indexes)
				if ok {
					curMSI[thisSel] = updated
				}
				return nil, ok
			}
		}

		_, ok := curMSI[thisSel].(map[string]any)
		if !ok {
			_, ok = curMSI[thisSel].(Map)
		}

		if (curMSI[thisSel] == nil || !ok) && len(indexes) == 0 && action == actionSet {
			curMSI[thisSel] = map[string]any{}
		}

		current, ok = curMSI[thisSel]
		if !ok {
			return nil, false
		}
	default:
		return nil, false
	}

	// do we need to access the item of an array?
	if len(indexes) > 0 {
		num := len(indexes)
		for num > 0 {
			num--
			index := indexes[num]
			indexes = indexes[:num]
			if array, ok := interSlice(current); ok {
				if index >= 0 && index < len(array) {
					if action == actionSet && nextSel == "" && num == 0 {
						array[index] = value
						return nil, true
					}
					current = array[index]
				} else {
					return nil, false
				}
			} else {
				return nil, false
			}
		}
	}

	if nextSel != "" {
		return access(current, nextSel, value, action)
	}
	return current, true
}

// deleteSliceIndex copies slices along an index path, which getIndex builds in
// reverse order. Nothing is copied or replaced unless the whole path exists.
func deleteSliceIndex(current any, indexes []int) (any, bool) {
	s := reflect.ValueOf(current)
	if s.Kind() != reflect.Slice {
		return nil, false
	}
	index := indexes[len(indexes)-1]
	if index < 0 || index >= s.Len() {
		return nil, false
	}
	if len(indexes) == 1 {
		out := reflect.MakeSlice(s.Type(), s.Len()-1, s.Len()-1)
		reflect.Copy(out, s.Slice(0, index))
		reflect.Copy(out.Slice(index, out.Len()), s.Slice(index+1, s.Len()))
		return out.Interface(), true
	}
	updated, ok := deleteSliceIndex(s.Index(index).Interface(), indexes[:len(indexes)-1])
	if !ok {
		return nil, false
	}
	out := reflect.MakeSlice(s.Type(), s.Len(), s.Len())
	reflect.Copy(out, s)
	out.Index(index).Set(reflect.ValueOf(updated))
	return out.Interface(), true
}

func interSlice(slice any) ([]any, bool) {
	if array, ok := slice.([]any); ok {
		return array, ok
	}

	s := reflect.ValueOf(slice)
	if s.Kind() != reflect.Slice {
		return nil, false
	}

	ret := make([]any, s.Len())

	for i := range ret {
		ret[i] = s.Index(i).Interface()
	}

	return ret, true
}
