// Package maps provides reusable functions for manipulating nested
// map[string]interface{} maps are common unmarshal products from
// various serializers such as json, yaml etc.
package maps

import (
	"fmt"
	"github.com/mitchellh/copystructure"
	"reflect"
	"strings"
)

// Flatten takes a map[string]interface{} and traverses it and flattens
// nested children into keys delimited by delim.
//
// It's important to note that all nested maps should be
// map[string]interface{} and not map[interface{}]interface{}.
// Use IntfaceKeysToStrings() to convert if necessary.
//
// eg: `{ "parent": { "child": 123 }}` becomes `{ "parent.child": 123 }`
// In addition, it keeps track of and returns a map of the delimited keypaths with
// a slice of key parts, for eg: { "parent.child": ["parent", "child"] }. This
// parts list is used to remember the key path's original structure to
// unflatten later.
func Flatten(m map[string]interface{}, keys []string, delim string) (map[string]interface{}, map[string][]string) {
	var (
		out    = make(map[string]interface{})
		keyMap = make(map[string][]string)
	)

	flatten(m, keys, delim, out, keyMap)
	return out, keyMap
}

func flatten(m map[string]interface{}, keys []string, delim string, out map[string]interface{}, keyMap map[string][]string) {
	for key, val := range m {
		// Copy the incoming key paths into a fresh list
		// and append the current key in the iteration.
		kp := make([]string, 0, len(keys)+1)
		kp = append(kp, keys...)
		kp = append(kp, key)

		switch cur := val.(type) {
		case map[string]interface{}:
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
// It's important to note that all nested maps should be
// map[string]interface{} and not map[interface{}]interface{}.
// Use IntfaceKeysToStrings() to convert if necessary.
func Unflatten(m map[string]interface{}, delim string) map[string]interface{} {
	out := make(map[string]interface{})

	// Iterate through the flat conf map.
	for k, v := range m {
		var (
			keys = strings.Split(k, delim)
			next = out
		)

		// Iterate through key parts, for eg:, parent.child.key
		// will be ["parent", "child", "key"]
		for _, k := range keys[:len(keys)-1] {
			sub, ok := next[k]
			if !ok {
				// If the key does not exist in the map, create it.
				sub = make(map[string]interface{})
				next[k] = sub
			}
			if n, ok := sub.(map[string]interface{}); ok {
				next = n
			}
		}

		// Assign the value.
		next[keys[len(keys)-1]] = v
	}
	return out
}

// Merge recursively merges map a into b (left to right), mutating
// and expanding map b. Note that there's no copying involved, so
// map b will retain references to map a.
//
// It's important to note that all nested maps should be
// map[string]interface{} and not map[interface{}]interface{}.
// Use IntfaceKeysToStrings() to convert if necessary.
func Merge(a, b map[string]interface{}) {
	for key, val := range a {
		// Does the key exist in the target map?
		// If no, add it and move on.
		bVal, ok := b[key]
		if !ok {
			b[key] = val
			continue
		}

		// If the incoming val is not a map, do a direct merge.
		if _, ok := val.(map[string]interface{}); !ok {
			b[key] = val
			continue
		}

		// The source key and target keys are both maps. Merge them.
		switch v := bVal.(type) {
		case map[string]interface{}:
			Merge(val.(map[string]interface{}), v)
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
// map[string]interface{} and not map[interface{}]interface{}.
// Use IntfaceKeysToStrings() to convert if necessary.
func MergeStrict(a, b map[string]interface{}) error {
	return mergeStrict(a, b, "")
}

func mergeStrict(a, b map[string]interface{}, fullKey string) error {
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
		if _, ok := val.(map[string]interface{}); !ok {
			if reflect.TypeOf(b[key]) == reflect.TypeOf(val) {
				b[key] = val
			} else {
				return fmt.Errorf("incorrect types at key %v, type %T != %T", fullKey, b[key], val)
			}
			continue
		}

		// The source key and target keys are both maps. Merge them.
		switch v := bVal.(type) {
		case map[string]interface{}:
			return mergeStrict(val.(map[string]interface{}), v, newFullKey)
		default:
			b[key] = val
		}
	}
	return nil
}

// Delete removes the entry present at a given path, from the map. The path
// is the key map slice, for eg:, parent.child.key -> [parent child key].
// Any empty, nested map on the path, is recursively deleted.
//
// It's important to note that all nested maps should be
// map[string]interface{} and not map[interface{}]interface{}.
// Use IntfaceKeysToStrings() to convert if necessary.
func Delete(mp map[string]interface{}, path []string) {
	next, ok := mp[path[0]]
	if ok {
		if len(path) == 1 {
			delete(mp, path[0])
			return
		}
		switch nval := next.(type) {
		case map[string]interface{}:
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
// map[string]interface{} and not map[interface{}]interface{}.
// Use IntfaceKeysToStrings() to convert if necessary.
func Search(mp map[string]interface{}, path []string) interface{} {
	next, ok := mp[path[0]]
	if ok {
		if len(path) == 1 {
			return next
		}
		switch next.(type) {
		case map[string]interface{}:
			return Search(next.(map[string]interface{}), path[1:])
		default:
			return nil
		} //
		// It's important to note that all nested maps should be
		// map[string]interface{} and not map[interface{}]interface{}.
		// Use IntfaceKeysToStrings() to convert if necessary.
	}
	return nil
}

// Copy returns a deep copy of a conf map.
//
// It's important to note that all nested maps should be
// map[string]interface{} and not map[interface{}]interface{}.
// Use IntfaceKeysToStrings() to convert if necessary.
func Copy(mp map[string]interface{}) map[string]interface{} {
	out, _ := copystructure.Copy(&mp)
	if res, ok := out.(*map[string]interface{}); ok {
		return *res
	}
	return map[string]interface{}{}
}

// IntfaceKeysToStrings recursively converts map[interface{}]interface{} to
// map[string]interface{}. Some parses such as YAML unmarshal return this.
func IntfaceKeysToStrings(mp map[string]interface{}) {
	for key, val := range mp {
		switch cur := val.(type) {
		case map[interface{}]interface{}:
			x := make(map[string]interface{})
			for k, v := range cur {
				x[fmt.Sprintf("%v", k)] = v
			}
			mp[key] = x
			IntfaceKeysToStrings(x)
		case []interface{}:
			for i, v := range cur {
				switch sub := v.(type) {
				case map[interface{}]interface{}:
					x := make(map[string]interface{})
					for k, v := range sub {
						x[fmt.Sprintf("%v", k)] = v
					}
					cur[i] = x
					IntfaceKeysToStrings(x)
				case map[string]interface{}:
					IntfaceKeysToStrings(sub)
				}
			}
		case map[string]interface{}:
			IntfaceKeysToStrings(cur)
		}
	}
}

// StringSliceToLookupMap takes a slice of strings and returns a lookup map
// with the slice values as keys with true values.
func StringSliceToLookupMap(s []string) map[string]bool {
	mp := make(map[string]bool, len(s))
	for _, v := range s {
		mp[v] = true
	}
	return mp
}

// Int64SliceToLookupMap takes a slice of int64s and returns a lookup map
// with the slice values as keys with true values.
func Int64SliceToLookupMap(s []int64) map[int64]bool {
	mp := make(map[int64]bool, len(s))
	for _, v := range s {
		mp[v] = true
	}
	return mp
}
