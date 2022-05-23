package jsonpatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

var errBadJSONDoc = fmt.Errorf("invalid JSON Document")

type JsonPatchOperation = Operation

type Operation struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value,omitempty"`
}

func (j *Operation) Json() string {
	b, _ := json.Marshal(j)
	return string(b)
}

func (j *Operation) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{")
	b.WriteString(fmt.Sprintf(`"op":"%s"`, j.Operation))
	b.WriteString(fmt.Sprintf(`,"path":"%s"`, j.Path))
	// Consider omitting Value for non-nullable operations.
	if j.Value != nil || j.Operation == "replace" || j.Operation == "add" {
		v, err := json.Marshal(j.Value)
		if err != nil {
			return nil, err
		}
		b.WriteString(`,"value":`)
		b.Write(v)
	}
	b.WriteString("}")
	return b.Bytes(), nil
}

type ByPath []Operation

func (a ByPath) Len() int           { return len(a) }
func (a ByPath) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByPath) Less(i, j int) bool { return a[i].Path < a[j].Path }

func NewOperation(op, path string, value interface{}) Operation {
	return Operation{Operation: op, Path: path, Value: value}
}

// CreatePatch creates a patch as specified in http://jsonpatch.com/
//
// 'a' is original, 'b' is the modified document. Both are to be given as json encoded content.
// The function will return an array of JsonPatchOperations
//
// An error will be returned if any of the two documents are invalid.
func CreatePatch(a, b []byte) ([]Operation, error) {
	var aI interface{}
	var bI interface{}
	err := json.Unmarshal(a, &aI)
	if err != nil {
		return nil, errBadJSONDoc
	}
	err = json.Unmarshal(b, &bI)
	if err != nil {
		return nil, errBadJSONDoc
	}
	return handleValues(aI, bI, "", []Operation{})
}

// Returns true if the values matches (must be json types)
// The types of the values must match, otherwise it will always return false
// If two map[string]interface{} are given, all elements must match.
func matchesValue(av, bv interface{}) bool {
	if reflect.TypeOf(av) != reflect.TypeOf(bv) {
		return false
	}
	switch at := av.(type) {
	case string:
		bt, ok := bv.(string)
		if ok && bt == at {
			return true
		}
	case float64:
		bt, ok := bv.(float64)
		if ok && bt == at {
			return true
		}
	case bool:
		bt, ok := bv.(bool)
		if ok && bt == at {
			return true
		}
	case map[string]interface{}:
		bt, ok := bv.(map[string]interface{})
		if !ok {
			return false
		}
		for key := range at {
			if !matchesValue(at[key], bt[key]) {
				return false
			}
		}
		for key := range bt {
			if !matchesValue(at[key], bt[key]) {
				return false
			}
		}
		return true
	case []interface{}:
		bt, ok := bv.([]interface{})
		if !ok {
			return false
		}
		if len(bt) != len(at) {
			return false
		}
		for key := range at {
			if !matchesValue(at[key], bt[key]) {
				return false
			}
		}
		for key := range bt {
			if !matchesValue(at[key], bt[key]) {
				return false
			}
		}
		return true
	}
	return false
}

// From http://tools.ietf.org/html/rfc6901#section-4 :
//
// Evaluation of each reference token begins by decoding any escaped
// character sequence.  This is performed by first transforming any
// occurrence of the sequence '~1' to '/', and then transforming any
// occurrence of the sequence '~0' to '~'.
//   TODO decode support:
//   var rfc6901Decoder = strings.NewReplacer("~1", "/", "~0", "~")

var rfc6901Encoder = strings.NewReplacer("~", "~0", "/", "~1")

func makePath(path string, newPart interface{}) string {
	key := rfc6901Encoder.Replace(fmt.Sprintf("%v", newPart))
	if path == "" {
		return "/" + key
	}
	if strings.HasSuffix(path, "/") {
		return path + key
	}
	return path + "/" + key
}

// diff returns the (recursive) difference between a and b as an array of JsonPatchOperations.
func diff(a, b map[string]interface{}, path string, patch []Operation) ([]Operation, error) {
	for key, bv := range b {
		p := makePath(path, key)
		av, ok := a[key]
		// value was added
		if !ok {
			patch = append(patch, NewOperation("add", p, bv))
			continue
		}
		// Types are the same, compare values
		var err error
		patch, err = handleValues(av, bv, p, patch)
		if err != nil {
			return nil, err
		}
	}
	// Now add all deleted values as nil
	for key := range a {
		_, found := b[key]
		if !found {
			p := makePath(path, key)

			patch = append(patch, NewOperation("remove", p, nil))
		}
	}
	return patch, nil
}

func handleValues(av, bv interface{}, p string, patch []Operation) ([]Operation, error) {
	{
		at := reflect.TypeOf(av)
		bt := reflect.TypeOf(bv)
		if at == nil && bt == nil {
			// do nothing
			return patch, nil
		} else if at != bt {
			// If types have changed, replace completely (preserves null in destination)
			return append(patch, NewOperation("replace", p, bv)), nil
		}
	}

	var err error
	switch at := av.(type) {
	case map[string]interface{}:
		bt := bv.(map[string]interface{})
		patch, err = diff(at, bt, p, patch)
		if err != nil {
			return nil, err
		}
	case string, float64, bool:
		if !matchesValue(av, bv) {
			patch = append(patch, NewOperation("replace", p, bv))
		}
	case []interface{}:
		bt := bv.([]interface{})
		if isSimpleArray(at) && isSimpleArray(bt) {
			patch = append(patch, compareEditDistance(at, bt, p)...)
		} else {
			n := min(len(at), len(bt))
			for i := len(at) - 1; i >= n; i-- {
				patch = append(patch, NewOperation("remove", makePath(p, i), nil))
			}
			for i := n; i < len(bt); i++ {
				patch = append(patch, NewOperation("add", makePath(p, i), bt[i]))
			}
			for i := 0; i < n; i++ {
				var err error
				patch, err = handleValues(at[i], bt[i], makePath(p, i), patch)
				if err != nil {
					return nil, err
				}
			}
		}
	default:
		panic(fmt.Sprintf("Unknown type:%T ", av))
	}
	return patch, nil
}

func isBasicType(a interface{}) bool {
	switch a.(type) {
	case string, float64, bool:
	default:
		return false
	}
	return true
}

func isSimpleArray(a []interface{}) bool {
	for i := range a {
		switch a[i].(type) {
		case string, float64, bool:
		default:
			val := reflect.ValueOf(a[i])
			if val.Kind() == reflect.Map {
				for _, k := range val.MapKeys() {
					av := val.MapIndex(k)
					if av.Kind() == reflect.Ptr || av.Kind() == reflect.Interface {
						if av.IsNil() {
							continue
						}
						av = av.Elem()
					}
					if av.Kind() != reflect.String && av.Kind() != reflect.Float64 && av.Kind() != reflect.Bool {
						return false
					}
				}
				return true
			}
			return false
		}
	}
	return true
}

// https://en.wikipedia.org/wiki/Wagner%E2%80%93Fischer_algorithm
// Adapted from https://github.com/texttheater/golang-levenshtein
func compareEditDistance(s, t []interface{}, p string) []Operation {
	m := len(s)
	n := len(t)

	d := make([][]int, m+1)
	for i := 0; i <= m; i++ {
		d[i] = make([]int, n+1)
		d[i][0] = i
	}
	for j := 0; j <= n; j++ {
		d[0][j] = j
	}

	for j := 1; j <= n; j++ {
		for i := 1; i <= m; i++ {
			if reflect.DeepEqual(s[i-1], t[j-1]) {
				d[i][j] = d[i-1][j-1] // no op required
			} else {
				del := d[i-1][j] + 1
				add := d[i][j-1] + 1
				rep := d[i-1][j-1] + 1
				d[i][j] = min(rep, min(add, del))
			}
		}
	}

	return backtrace(s, t, p, m, n, d)
}

func min(x int, y int) int {
	if y < x {
		return y
	}
	return x
}

func backtrace(s, t []interface{}, p string, i int, j int, matrix [][]int) []Operation {
	if i > 0 && matrix[i-1][j]+1 == matrix[i][j] {
		op := NewOperation("remove", makePath(p, i-1), nil)
		return append([]Operation{op}, backtrace(s, t, p, i-1, j, matrix)...)
	}
	if j > 0 && matrix[i][j-1]+1 == matrix[i][j] {
		op := NewOperation("add", makePath(p, i), t[j-1])
		return append([]Operation{op}, backtrace(s, t, p, i, j-1, matrix)...)
	}
	if i > 0 && j > 0 && matrix[i-1][j-1]+1 == matrix[i][j] {
		if isBasicType(s[0]) {
			op := NewOperation("replace", makePath(p, i-1), t[j-1])
			return append([]Operation{op}, backtrace(s, t, p, i-1, j-1, matrix)...)
		}

		p2, _ := handleValues(s[i-1], t[j-1], makePath(p, i-1), []Operation{})
		return append(p2, backtrace(s, t, p, i-1, j-1, matrix)...)
	}
	if i > 0 && j > 0 && matrix[i-1][j-1] == matrix[i][j] {
		return backtrace(s, t, p, i-1, j-1, matrix)
	}
	return []Operation{}
}
