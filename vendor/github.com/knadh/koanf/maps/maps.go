// Package maps provides reusable functions for manipulating nested
// map[string]any maps are common unmarshal products from
// various serializers such as json, yaml etc.
package maps

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mitchellh/copystructure"
)

// Flatten takes a map[string]any and traverses it and flattens
// nested children into keys delimited by delim.
//
// It's important to note that all nested maps should be
// map[string]any and not map[any]any.
// Use IntfaceKeysToStrings() to convert if necessary.
//
// eg: `{ "parent": { "child": 123 }}` becomes `{ "parent.child": 123 }`
// In addition, it keeps track of and returns a map of the delimited keypaths with
// a slice of key parts, for eg: { "parent.child": ["parent", "child"] }. This
// parts list is used to remember the key path's original structure to
// unflatten later.
func Flatten(m map[string]any, keys []string, delim string) (map[string]any, map[string][]string) {
	var (
		out    = make(map[string]any)
		keyMap = make(map[string][]string)
	)

	flatten(m, keys, delim, out, keyMap)
	return out, keyMap
}

func flatten(m map[string]any, keys []string, delim string, out map[string]any, keyMap map[string][]string) {
	for key, val := range m {
		// Copy the incoming key paths into a fresh list
		// and append the current key in the iteration.
		kp := make([]string, 0, len(keys)+1)
		kp = append(kp, keys...)
		kp = append(kp, key)

		switch cur := val.(type) {
		case map[string]any:
			// Empty map.
			if len(cur) == 0 {
				newKey := strings.Join(kp, delim)
				out[newKey] = val
				keyMap[newKey] = kp
				continue
			}

			// It's a nested map. Flatten it recursively.
			flatten(cur, kp, delim, out, keyMap)
		default:
			newKey := strings.Join(kp, delim)
			out[newKey] = val
			keyMap[newKey] = kp
		}
	}
}

// Unflatten takes a flattened key:value map (non-nested with delimited keys)
// and returns a nested map where the keys are split into hierarchies by the given
// delimiter. For instance, `parent.child.key: 1` to `{parent: {child: {key: 1}}}`
//
// Keys are sorted by length to make merge-order deterministic so that nested keys
// don't end up overwriting their parent keys. eg: `parent.child` -> `parent`.
//
// It's important to note that all nested maps should be
// map[string]any and not map[any]any.
// Use IntfaceKeysToStrings() to convert if necessary.
func Unflatten(m map[string]any, delim string) map[string]any {
	out := make(map[string]any)

	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)

	for _, k := range ks {
		var (
			keys []string
			next = out
		)

		if delim != "" {
			keys = strings.Split(k, delim)
		} else {
			keys = []string{k}
		}

		// Iterate through key parts, for eg:, parent.child.key
		// will be ["parent", "child", "key"]
		for _, k := range keys[:len(keys)-1] {
			// Create the key if it doesn't exist.
			sub, ok := next[k].(map[string]any)
			if !ok {
				sub = make(map[string]any)
				next[k] = sub
			}
			next = sub
		}

		// Assign the value.
		next[keys[len(keys)-1]] = m[k]
	}
	return out
}

// Merge recursively merges map a into b (left to right), mutating
// and expanding map b. Note that there's no copying involved, so
// map b will retain references to map a.
//
// It's important to note that all nested maps should be
// map[string]any and not map[any]any.
// Use IntfaceKeysToStrings() to convert if necessary.
func Merge(a, b map[string]any) {
	for key, val := range a {
		// Does the key exist in the target map?
		// If no, add it and move on.
		bVal, ok := b[key]
		if !ok {
			b[key] = val
			continue
		}

		// If the incoming val is not a map, do a direct merge.
		if _, ok := val.(map[string]any); !ok {
			b[key] = val
			continue
		}

		// The source key and target keys are both maps. Merge them.
		switch v := bVal.(type) {
		case map[string]any:
			Merge(val.(map[string]any), v)
		default:
			b[key] = val
		}
	}
}

// MergeStrict recursively merges map a into b (left to right), mutating
// and expanding map b. Note that there's no copying involved, so
// map b will retain references to map a.
// If an equal key in either of the maps has a different value type, it will return the first error.
//
// It's important to note that all nested maps should be
// map[string]any and not map[any]any.
// Use IntfaceKeysToStrings() to convert if necessary.
func MergeStrict(a, b map[string]any) error {
	return mergeStrict(a, b, "")
}

func mergeStrict(a, b map[string]any, fullKey string) error {
	for key, val := range a {
		// Does the key exist in the target map?
		// If no, add it and move on.
		bVal, ok := b[key]
		if !ok {
			b[key] = val
			continue
		}

		newFullKey := key
		if fullKey != "" {
			newFullKey = fmt.Sprintf("%v.%v", fullKey, key)
		}

		// If the incoming val is not a map, do a direct merge between the same types.
		if _, ok := val.(map[string]any); !ok {
			if reflect.TypeOf(b[key]) == reflect.TypeOf(val) {
				b[key] = val
			} else {
				return fmt.Errorf("incorrect types at key %v, type %T != %T", newFullKey, b[key], val)
			}
			continue
		}

		// The source key and target keys are both maps. Merge them.
		switch v := bVal.(type) {
		case map[string]any:
			if err := mergeStrict(val.(map[string]any), v, newFullKey); err != nil {
				return err
			}
		default:
			// The incoming value is a map but the target value is not,
			// which is a type mismatch.
			return fmt.Errorf("incorrect types at key %v, type %T != %T", newFullKey, b[key], val)
		}
	}
	return nil
}

// Delete removes the entry present at a given path, from the map. The path
// is the key map slice, for eg:, parent.child.key -> [parent child key].
// Any empty, nested map on the path, is recursively deleted.
//
// It's important to note that all nested maps should be
// map[string]any and not map[any]any.
// Use IntfaceKeysToStrings() to convert if necessary.
func Delete(mp map[string]any, path []string) {
	next, ok := mp[path[0]]
	if ok {
		if len(path) == 1 {
			delete(mp, path[0])
			return
		}
		switch nval := next.(type) {
		case map[string]any:
			Delete(nval, path[1:])
			// Delete map if it has no keys.
			if len(nval) == 0 {
				delete(mp, path[0])
			}
		}
	}
}

// Search recursively searches a map for a given path. The path is
// the key map slice, for eg:, parent.child.key -> [parent child key].
//
// It's important to note that all nested maps should be
// map[string]any and not map[any]any.
// Use IntfaceKeysToStrings() to convert if necessary.
func Search(mp map[string]any, path []string) any {
	next, ok := mp[path[0]]
	if ok {
		if len(path) == 1 {
			return next
		}
		switch m := next.(type) {
		case map[string]any:
			return Search(m, path[1:])
		default:
			return nil
		} //
		// It's important to note that all nested maps should be
		// map[string]any and not map[any]any.
		// Use IntfaceKeysToStrings() to convert if necessary.
	}
	return nil
}

// Copy returns a deep copy of a conf map.
//
// It's important to note that all nested maps should be
// map[string]any and not map[any]any.
// Use IntfaceKeysToStrings() to convert if necessary.
func Copy(mp map[string]any) map[string]any {
	out, _ := copystructure.Copy(&mp)
	if res, ok := out.(*map[string]any); ok {
		return *res
	}
	return map[string]any{}
}

// IntfaceKeysToStrings recursively converts map[any]any to
// map[string]any. Some parses such as YAML unmarshal return this.
func IntfaceKeysToStrings(mp map[string]any) {
	for key, val := range mp {
		switch cur := val.(type) {
		case map[any]any:
			x := make(map[string]any)
			for k, v := range cur {
				x[fmt.Sprintf("%v", k)] = v
			}
			mp[key] = x
			IntfaceKeysToStrings(x)
		case []any:
			for i, v := range cur {
				switch sub := v.(type) {
				case map[any]any:
					x := make(map[string]any)
					for k, v := range sub {
						x[fmt.Sprintf("%v", k)] = v
					}
					cur[i] = x
					IntfaceKeysToStrings(x)
				case map[string]any:
					IntfaceKeysToStrings(sub)
				}
			}
		case map[string]any:
			IntfaceKeysToStrings(cur)
		}
	}
}

// StringSliceToLookupMap takes a slice of strings and returns a lookup map
// with the slice values as keys with true values.
func StringSliceToLookupMap(s []string) map[string]bool {
	return sliceToLookupMap(s)
}

// Int64SliceToLookupMap takes a slice of int64s and returns a lookup map
// with the slice values as keys with true values.
func Int64SliceToLookupMap(s []int64) map[int64]bool {
	return sliceToLookupMap(s)
}

func sliceToLookupMap[T string | int64](s []T) map[T]bool {
	mp := make(map[T]bool, len(s))
	for _, v := range s {
		mp[v] = true
	}
	return mp
}
