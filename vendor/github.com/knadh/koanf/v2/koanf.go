package koanf

import (
	"bytes"
	"encoding"
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/maps"
	"github.com/mitchellh/copystructure"
)

// Koanf is the configuration apparatus.
type Koanf struct {
	confMap     map[string]interface{}
	confMapFlat map[string]interface{}
	keyMap      KeyMap
	conf        Conf
}

// Conf is the Koanf configuration.
type Conf struct {
	// Delim is the delimiter to use
	// when specifying config key paths, for instance a . for `parent.child.key`
	// or a / for `parent/child/key`.
	Delim string

	// StrictMerge makes the merging behavior strict.
	// Meaning when loading two files that have the same key,
	// the first loaded file will define the desired type, and if the second file loads
	// a different type will cause an error.
	StrictMerge bool
}

// KeyMap represents a map of flattened delimited keys and the non-delimited
// parts as their slices. For nested keys, the map holds all levels of path combinations.
// For example, the nested structure `parent -> child -> key` will produce the map:
// parent.child.key => [parent, child, key]
// parent.child => [parent, child]
// parent => [parent]
type KeyMap map[string][]string

// UnmarshalConf represents configuration options used by
// Unmarshal() to unmarshal conf maps into arbitrary structs.
type UnmarshalConf struct {
	// Tag is the struct field tag to unmarshal.
	// `koanf` is used if left empty.
	Tag string

	// If this is set to true, instead of unmarshalling nested structures
	// based on the key path, keys are taken literally to unmarshal into
	// a flat struct. For example:
	// ```
	// type MyStuff struct {
	// 	Child1Name string `koanf:"parent1.child1.name"`
	// 	Child2Name string `koanf:"parent2.child2.name"`
	// 	Type       string `koanf:"json"`
	// }
	// ```
	FlatPaths     bool
	DecoderConfig *mapstructure.DecoderConfig
}

// New returns a new instance of Koanf. delim is the delimiter to use
// when specifying config key paths, for instance a . for `parent.child.key`
// or a / for `parent/child/key`.
func New(delim string) *Koanf {
	return NewWithConf(Conf{
		Delim:       delim,
		StrictMerge: false,
	})
}

// NewWithConf returns a new instance of Koanf based on the Conf.
func NewWithConf(conf Conf) *Koanf {
	return &Koanf{
		confMap:     make(map[string]interface{}),
		confMapFlat: make(map[string]interface{}),
		keyMap:      make(KeyMap),
		conf:        conf,
	}
}

// Load takes a Provider that either provides a parsed config map[string]interface{}
// in which case pa (Parser) can be nil, or raw bytes to be parsed, where a Parser
// can be provided to parse. Additionally, options can be passed which modify the
// load behavior, such as passing a custom merge function.
func (ko *Koanf) Load(p Provider, pa Parser, opts ...Option) error {
	var (
		mp  map[string]interface{}
		err error
	)

	if p == nil {
		return fmt.Errorf("load received a nil provider")
	}

	// No Parser is given. Call the Provider's Read() method to get
	// the config map.
	if pa == nil {
		mp, err = p.Read()
		if err != nil {
			return err
		}
	} else {
		// There's a Parser. Get raw bytes from the Provider to parse.
		b, err := p.ReadBytes()
		if err != nil {
			return err
		}
		mp, err = pa.Unmarshal(b)
		if err != nil {
			return err
		}
	}

	return ko.merge(mp, newOptions(opts))
}

// Keys returns the slice of all flattened keys in the loaded configuration
// sorted alphabetically.
func (ko *Koanf) Keys() []string {
	out := make([]string, 0, len(ko.confMapFlat))
	for k := range ko.confMapFlat {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// KeyMap returns a map of flattened keys and the individual parts of the
// key as slices. eg: "parent.child.key" => ["parent", "child", "key"].
func (ko *Koanf) KeyMap() KeyMap {
	out := make(KeyMap, len(ko.keyMap))
	for key, parts := range ko.keyMap {
		out[key] = make([]string, len(parts))
		copy(out[key], parts)
	}
	return out
}

// All returns a map of all flattened key paths and their values.
// Note that it uses maps.Copy to create a copy that uses
// json.Marshal which changes the numeric types to float64.
func (ko *Koanf) All() map[string]interface{} {
	return maps.Copy(ko.confMapFlat)
}

// Raw returns a copy of the full raw conf map.
// Note that it uses maps.Copy to create a copy that uses
// json.Marshal which changes the numeric types to float64.
func (ko *Koanf) Raw() map[string]interface{} {
	return maps.Copy(ko.confMap)
}

// Sprint returns a key -> value string representation
// of the config map with keys sorted alphabetically.
func (ko *Koanf) Sprint() string {
	b := bytes.Buffer{}
	for _, k := range ko.Keys() {
		b.WriteString(fmt.Sprintf("%s -> %v\n", k, ko.confMapFlat[k]))
	}
	return b.String()
}

// Print prints a key -> value string representation
// of the config map with keys sorted alphabetically.
func (ko *Koanf) Print() {
	fmt.Print(ko.Sprint())
}

// Cut cuts the config map at a given key path into a sub map and
// returns a new Koanf instance with the cut config map loaded.
// For instance, if the loaded config has a path that looks like
// parent.child.sub.a.b, `Cut("parent.child")` returns a new Koanf
// instance with the config map `sub.a.b` where everything above
// `parent.child` are cut out.
func (ko *Koanf) Cut(path string) *Koanf {
	out := make(map[string]interface{})

	// Cut only makes sense if the requested key path is a map.
	if v, ok := ko.Get(path).(map[string]interface{}); ok {
		out = v
	}

	n := New(ko.conf.Delim)
	_ = n.merge(out, new(options))
	return n
}

// Copy returns a copy of the Koanf instance.
func (ko *Koanf) Copy() *Koanf {
	return ko.Cut("")
}

// Merge merges the config map of a given Koanf instance into
// the current instance.
func (ko *Koanf) Merge(in *Koanf) error {
	return ko.merge(in.Raw(), new(options))
}

// MergeAt merges the config map of a given Koanf instance into
// the current instance as a sub map, at the given key path.
// If all or part of the key path is missing, it will be created.
// If the key path is `""`, this is equivalent to Merge.
func (ko *Koanf) MergeAt(in *Koanf, path string) error {
	// No path. Merge the two config maps.
	if path == "" {
		return ko.Merge(in)
	}

	// Unflatten the config map with the given key path.
	n := maps.Unflatten(map[string]interface{}{
		path: in.Raw(),
	}, ko.conf.Delim)

	return ko.merge(n, new(options))
}

// Set sets the value at a specific key.
func (ko *Koanf) Set(key string, val interface{}) error {
	// Unflatten the config map with the given key path.
	n := maps.Unflatten(map[string]interface{}{
		key: val,
	}, ko.conf.Delim)

	return ko.merge(n, new(options))
}

// Marshal takes a Parser implementation and marshals the config map into bytes,
// for example, to TOML or JSON bytes.
func (ko *Koanf) Marshal(p Parser) ([]byte, error) {
	return p.Marshal(ko.Raw())
}

// Unmarshal unmarshals a given key path into the given struct using
// the mapstructure lib. If no path is specified, the whole map is unmarshalled.
// `koanf` is the struct field tag used to match field names. To customize,
// use UnmarshalWithConf(). It uses the mitchellh/mapstructure package.
func (ko *Koanf) Unmarshal(path string, o interface{}) error {
	return ko.UnmarshalWithConf(path, o, UnmarshalConf{})
}

// UnmarshalWithConf is like Unmarshal but takes configuration params in UnmarshalConf.
// See mitchellh/mapstructure's DecoderConfig for advanced customization
// of the unmarshal behaviour.
func (ko *Koanf) UnmarshalWithConf(path string, o interface{}, c UnmarshalConf) error {
	if c.DecoderConfig == nil {
		c.DecoderConfig = &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
				textUnmarshalerHookFunc()),
			Metadata:         nil,
			WeaklyTypedInput: true,
		}
	}

	c.DecoderConfig.Result = o

	if c.Tag == "" {
		c.DecoderConfig.TagName = "koanf"
	} else {
		c.DecoderConfig.TagName = c.Tag
	}

	d, err := mapstructure.NewDecoder(c.DecoderConfig)
	if err != nil {
		return err
	}

	// Unmarshal using flat key paths.
	mp := ko.Get(path)
	if c.FlatPaths {
		if f, ok := mp.(map[string]interface{}); ok {
			fmp, _ := maps.Flatten(f, nil, ko.conf.Delim)
			mp = fmp
		}
	}

	return d.Decode(mp)
}

// Delete removes all nested values from a given path.
// Clears all keys/values if no path is specified.
// Every empty, key on the path, is recursively deleted.
func (ko *Koanf) Delete(path string) {
	// No path. Erase the entire map.
	if path == "" {
		ko.confMap = make(map[string]interface{})
		ko.confMapFlat = make(map[string]interface{})
		ko.keyMap = make(KeyMap)
		return
	}

	// Does the path exist?
	p, ok := ko.keyMap[path]
	if !ok {
		return
	}
	maps.Delete(ko.confMap, p)

	// Update the flattened version as well.
	ko.confMapFlat, ko.keyMap = maps.Flatten(ko.confMap, nil, ko.conf.Delim)
	ko.keyMap = populateKeyParts(ko.keyMap, ko.conf.Delim)
}

// Get returns the raw, uncast interface{} value of a given key path
// in the config map. If the key path does not exist, nil is returned.
func (ko *Koanf) Get(path string) interface{} {
	// No path. Return the whole conf map.
	if path == "" {
		return ko.Raw()
	}

	// Does the path exist?
	p, ok := ko.keyMap[path]
	if !ok {
		return nil
	}
	res := maps.Search(ko.confMap, p)

	// Non-reference types are okay to return directly.
	// Other types are "copied" with maps.Copy or json.Marshal
	// that change the numeric types to float64.

	switch v := res.(type) {
	case int, int8, int16, int32, int64, float32, float64, string, bool:
		return v
	case map[string]interface{}:
		return maps.Copy(v)
	}

	out, _ := copystructure.Copy(&res)
	if ptrOut, ok := out.(*interface{}); ok {
		return *ptrOut
	}
	return out
}

// Slices returns a list of Koanf instances constructed out of a
// []map[string]interface{} interface at the given path.
func (ko *Koanf) Slices(path string) []*Koanf {
	out := []*Koanf{}
	if path == "" {
		return out
	}

	// Does the path exist?
	sl, ok := ko.Get(path).([]interface{})
	if !ok {
		return out
	}

	for _, s := range sl {
		mp, ok := s.(map[string]interface{})
		if !ok {
			continue
		}

		k := New(ko.conf.Delim)
		_ = k.merge(mp, new(options))
		out = append(out, k)
	}

	return out
}

// Exists returns true if the given key path exists in the conf map.
func (ko *Koanf) Exists(path string) bool {
	_, ok := ko.keyMap[path]
	return ok
}

// MapKeys returns a sorted string list of keys in a map addressed by the
// given path. If the path is not a map, an empty string slice is
// returned.
func (ko *Koanf) MapKeys(path string) []string {
	var (
		out = []string{}
		o   = ko.Get(path)
	)
	if o == nil {
		return out
	}

	mp, ok := o.(map[string]interface{})
	if !ok {
		return out
	}
	out = make([]string, 0, len(mp))
	for k := range mp {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Delim returns delimiter in used by this instance of Koanf.
func (ko *Koanf) Delim() string {
	return ko.conf.Delim
}

func (ko *Koanf) merge(c map[string]interface{}, opts *options) error {
	maps.IntfaceKeysToStrings(c)
	if opts.merge != nil {
		if err := opts.merge(c, ko.confMap); err != nil {
			return err
		}
	} else if ko.conf.StrictMerge {
		if err := maps.MergeStrict(c, ko.confMap); err != nil {
			return err
		}
	} else {
		maps.Merge(c, ko.confMap)
	}

	// Maintain a flattened version as well.
	ko.confMapFlat, ko.keyMap = maps.Flatten(ko.confMap, nil, ko.conf.Delim)
	ko.keyMap = populateKeyParts(ko.keyMap, ko.conf.Delim)

	return nil
}

// toInt64 takes an interface value and if it is an integer type,
// converts and returns int64. If it's any other type,
// forces it to a string and attempts to do a strconv.Atoi
// to get an integer out.
func toInt64(v interface{}) (int64, error) {
	switch i := v.(type) {
	case int:
		return int64(i), nil
	case int8:
		return int64(i), nil
	case int16:
		return int64(i), nil
	case int32:
		return int64(i), nil
	case int64:
		return i, nil
	}

	// Force it to a string and try to convert.
	f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
	if err != nil {
		return 0, err
	}

	return int64(f), nil
}

// toInt64 takes a `v interface{}` value and if it is a float type,
// converts and returns a `float64`. If it's any other type, forces it to a
// string and attempts to get a float out using `strconv.ParseFloat`.
func toFloat64(v interface{}) (float64, error) {
	switch i := v.(type) {
	case float32:
		return float64(i), nil
	case float64:
		return i, nil
	}

	// Force it to a string and try to convert.
	f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
	if err != nil {
		return f, err
	}

	return f, nil
}

// toBool takes an interface value and if it is a bool type,
// returns it. If it's any other type, forces it to a string and attempts
// to parse it as a bool using strconv.ParseBool.
func toBool(v interface{}) (bool, error) {
	if b, ok := v.(bool); ok {
		return b, nil
	}

	// Force it to a string and try to convert.
	b, err := strconv.ParseBool(fmt.Sprintf("%v", v))
	if err != nil {
		return b, err
	}
	return b, nil
}

// populateKeyParts iterates a key map and generates all possible
// traversal paths. For instance, `parent.child.key` generates
// `parent`, and `parent.child`.
func populateKeyParts(m KeyMap, delim string) KeyMap {
	out := make(KeyMap, len(m)) // The size of the result is at very least same to KeyMap
	for _, parts := range m {
		// parts is a slice of [parent, child, key]
		var nk string

		for i := range parts {
			if i == 0 {
				// On first iteration only use first part
				nk = parts[i]
			} else {
				// If nk already contains a part (e.g. `parent`) append delim + `child`
				nk += delim + parts[i]
			}
			if _, ok := out[nk]; ok {
				continue
			}
			out[nk] = make([]string, i+1)
			copy(out[nk], parts[0:i+1])
		}
	}
	return out
}

// textUnmarshalerHookFunc is a fixed version of mapstructure.TextUnmarshallerHookFunc.
// This hook allows to additionally unmarshal text into custom string types that implement the encoding.Text(Un)Marshaler interface(s).
func textUnmarshalerHookFunc() mapstructure.DecodeHookFuncType {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{},
	) (interface{}, error) {
		if f.Kind() != reflect.String {
			return data, nil
		}
		result := reflect.New(t).Interface()
		unmarshaller, ok := result.(encoding.TextUnmarshaler)
		if !ok {
			return data, nil
		}

		// default text representation is the actual value of the `from` string
		var (
			dataVal = reflect.ValueOf(data)
			text    = []byte(dataVal.String())
		)
		if f.Kind() == t.Kind() {
			// source and target are of underlying type string
			var (
				err    error
				ptrVal = reflect.New(dataVal.Type())
			)
			if !ptrVal.Elem().CanSet() {
				// cannot set, skip, this should not happen
				if err := unmarshaller.UnmarshalText(text); err != nil {
					return nil, err
				}
				return result, nil
			}
			ptrVal.Elem().Set(dataVal)

			// We need to assert that both, the value type and the pointer type
			// do (not) implement the TextMarshaller interface before proceeding and simply
			// using the string value of the string type.
			// it might be the case that the internal string representation differs from
			// the (un)marshalled string.

			for _, v := range []reflect.Value{dataVal, ptrVal} {
				if marshaller, ok := v.Interface().(encoding.TextMarshaler); ok {
					text, err = marshaller.MarshalText()
					if err != nil {
						return nil, err
					}
					break
				}
			}
		}

		// text is either the source string's value or the source string type's marshaled value
		// which may differ from its internal string value.
		if err := unmarshaller.UnmarshalText(text); err != nil {
			return nil, err
		}
		return result, nil
	}
}
