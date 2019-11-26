package main

import (
	"reflect"
	"testing"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
)

type entry struct {
	lbs  model.LabelSet
	line string
	ts   time.Time
}

type recorder struct {
	*entry
}

func (r *recorder) Handle(labels model.LabelSet, time time.Time, e string) error {
	r.entry = &entry{
		labels,
		e,
		time,
	}
	return nil
}

func (r *recorder) toEntry() *entry { return r.entry }

func (r *recorder) Stop() {}

var now = time.Now()

func Test_loki_sendRecord(t *testing.T) {
	var simpleRecordFixture = map[interface{}]interface{}{
		"foo":   "bar",
		"bar":   500,
		"error": make(chan struct{}),
	}
	var mapRecordFixture = map[interface{}]interface{}{
		// lots of key/value pairs in map to increase chances of test hitting in case of unsorted map marshalling
		"A": "A",
		"B": "B",
		"C": "C",
		"D": "D",
		"E": "E",
		"F": "F",
		"G": "G",
		"H": "H",
	}
	var byteArrayRecordFixture = map[interface{}]interface{}{
		"label": "label",
		"outer": []byte("foo"),
		"map": map[interface{}]interface{}{
			"inner": []byte("bar"),
		},
	}
	var mixedTypesRecordFixture = map[interface{}]interface{}{
		"label": "label",
		"int":   42,
		"float": 42.42,
		"array": []interface{}{42, 42.42, "foo"},
		"map": map[interface{}]interface{}{
			"nested": map[interface{}]interface{}{
				"foo":     "bar",
				"invalid": []byte("a\xc5z"),
			},
		},
	}

	tests := []struct {
		name   string
		cfg    *config
		record map[interface{}]interface{}
		want   *entry
	}{
		{"map to JSON", &config{labelKeys: []string{"A"}, lineFormat: jsonFormat}, mapRecordFixture, &entry{model.LabelSet{"A": "A"}, `{"B":"B","C":"C","D":"D","E":"E","F":"F","G":"G","H":"H"}`, now}},
		{"map to kvPairFormat", &config{labelKeys: []string{"A"}, lineFormat: kvPairFormat}, mapRecordFixture, &entry{model.LabelSet{"A": "A"}, `B=B C=C D=D E=E F=F G=G H=H`, now}},
		{"not enough records", &config{labelKeys: []string{"foo"}, lineFormat: jsonFormat, removeKeys: []string{"bar", "error"}}, simpleRecordFixture, nil},
		{"labels", &config{labelKeys: []string{"bar", "fake"}, lineFormat: jsonFormat, removeKeys: []string{"fuzz", "error"}}, simpleRecordFixture, &entry{model.LabelSet{"bar": "500"}, `{"foo":"bar"}`, now}},
		{"remove key", &config{labelKeys: []string{"fake"}, lineFormat: jsonFormat, removeKeys: []string{"foo", "error", "fake"}}, simpleRecordFixture, &entry{model.LabelSet{}, `{"bar":500}`, now}},
		// jsoniter.ConfigCompatibleWithStandardLibrary has an issue passing errors from inside a map:
		// https://github.com/json-iterator/go/issues/388 -- fix already proposed upstream (#422)
		//{"error", &config{labelKeys: []string{"fake"}, lineFormat: jsonFormat, removeKeys: []string{"foo"}}, simpleRecordFixture, &entry{model.LabelSet{}, `{"bar":500,"error":}`, now}},
		{"key value", &config{labelKeys: []string{"fake"}, lineFormat: kvPairFormat, removeKeys: []string{"foo", "error", "fake"}}, simpleRecordFixture, &entry{model.LabelSet{}, `bar=500`, now}},
		{"single", &config{labelKeys: []string{"fake"}, dropSingleKey: true, lineFormat: kvPairFormat, removeKeys: []string{"foo", "error", "fake"}}, simpleRecordFixture, &entry{model.LabelSet{}, `500`, now}},
		{"labelmap", &config{labelMap: map[string]interface{}{"bar": "other"}, lineFormat: jsonFormat, removeKeys: []string{"bar", "error"}}, simpleRecordFixture, &entry{model.LabelSet{"other": "500"}, `{"foo":"bar"}`, now}},
		{"byte array", &config{labelKeys: []string{"label"}, lineFormat: jsonFormat}, byteArrayRecordFixture, &entry{model.LabelSet{"label": "label"}, `{"map":{"inner":"bar"},"outer":"foo"}`, now}},
		{"mixed types", &config{labelKeys: []string{"label"}, lineFormat: jsonFormat}, mixedTypesRecordFixture, &entry{model.LabelSet{"label": "label"}, `{"array":[42,42.42,"foo"],"float":42.42,"int":42,"map":{"nested":{"foo":"bar","invalid":"a\ufffdz"}}}`, now}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			l := &loki{
				cfg:    tt.cfg,
				client: rec,
				logger: logger,
			}
			_ = l.sendRecord(tt.record, now)
			got := rec.toEntry()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sendRecord() want:%v got:%v", tt.want, got)
			}
		})
	}
}

func Test_createLine(t *testing.T) {
	tests := []struct {
		name    string
		records map[string]interface{}
		f       format
		want    string
		wantErr bool
	}{

		{"json", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, jsonFormat, `{"foo":"bar","bar":{"bizz":"bazz"}}`, false},
		{"json with number", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": 20}}, jsonFormat, `{"foo":"bar","bar":{"bizz":20}}`, false},
		// jsoniter.ConfigCompatibleWithStandardLibrary has an issue passing errors from inside a map:
		// https://github.com/json-iterator/go/issues/388 -- fix already proposed upstream (#422)
		// {"bad json", map[string]interface{}{"foo": make(chan interface{})}, jsonFormat, "", true},
		{"kv", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, kvPairFormat, `bar=map[bizz:bazz] foo=bar`, false},
		{"kv with number", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": 20}, "decimal": 12.2}, kvPairFormat, `bar=map[bizz:20] decimal=12.2 foo=bar`, false},
		{"kv with nil", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": 20}, "null": nil}, kvPairFormat, `bar=map[bizz:20] foo=bar null=<nil>`, false},
		{"kv empty", map[string]interface{}{}, kvPairFormat, ``, false},
		{"bad format", nil, format(3), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createLine(tt.records, tt.f)
			if (err != nil) != tt.wantErr {
				t.Errorf("createLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if tt.f == jsonFormat {
				compareJson(t, got, tt.want)
			} else {
				if got != tt.want {
					t.Errorf("createLine() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// compareJson unmarshal both string to map[string]interface compare json result.
// we can't compare string to string as jsoniter doesn't ensure field ordering.
func compareJson(t *testing.T, got, want string) {
	var w map[string]interface{}
	err := jsoniter.Unmarshal([]byte(want), &w)
	if err != nil {
		t.Errorf("failed to unmarshal string: %s", err)
	}
	var g map[string]interface{}
	err = jsoniter.Unmarshal([]byte(got), &g)
	if err != nil {
		t.Errorf("failed to unmarshal string: %s", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("compareJson() = %v, want %v", g, w)
	}
}

func Test_removeKeys(t *testing.T) {
	tests := []struct {
		name     string
		records  map[string]interface{}
		expected map[string]interface{}
		keys     []string
	}{
		{"remove all keys", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, map[string]interface{}{}, []string{"foo", "bar"}},
		{"remove none", map[string]interface{}{"foo": "bar"}, map[string]interface{}{"foo": "bar"}, []string{}},
		{"remove not existing", map[string]interface{}{"foo": "bar"}, map[string]interface{}{"foo": "bar"}, []string{"bar"}},
		{"remove one", map[string]interface{}{"foo": "bar", "bazz": "buzz"}, map[string]interface{}{"foo": "bar"}, []string{"bazz"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removeKeys(tt.records, tt.keys)
			if !reflect.DeepEqual(tt.expected, tt.records) {
				t.Errorf("removeKeys() = %v, want %v", tt.records, tt.expected)
			}
		})
	}
}

func Test_extractLabels(t *testing.T) {
	tests := []struct {
		name    string
		records map[string]interface{}
		keys    []string
		want    model.LabelSet
	}{
		{"single string", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, []string{"foo"}, model.LabelSet{"foo": "bar"}},
		{"multiple", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, []string{"foo", "bar"}, model.LabelSet{"foo": "bar", "bar": "map[bizz:bazz]"}},
		{"nil", map[string]interface{}{"foo": nil}, []string{"foo"}, model.LabelSet{"foo": "<nil>"}},
		{"none", map[string]interface{}{"foo": nil}, []string{}, model.LabelSet{}},
		{"missing", map[string]interface{}{"foo": "bar"}, []string{"foo", "buzz"}, model.LabelSet{"foo": "bar"}},
		{"skip invalid", map[string]interface{}{"foo.blah": "bar", "bar": "a\xc5z"}, []string{"foo.blah", "bar"}, model.LabelSet{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractLabels(tt.records, tt.keys); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_toStringMap(t *testing.T) {
	tests := []struct {
		name   string
		record map[interface{}]interface{}
		want   map[string]interface{}
	}{
		{"already string", map[interface{}]interface{}{"string": "foo", "bar": []byte("buzz")}, map[string]interface{}{"string": "foo", "bar": "buzz"}},
		{"skip non string", map[interface{}]interface{}{"string": "foo", 1.0: []byte("buzz")}, map[string]interface{}{"string": "foo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toStringMap(tt.record); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("toStringMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_labelMapping(t *testing.T) {
	tests := []struct {
		name    string
		records map[string]interface{}
		mapping map[string]interface{}
		want    model.LabelSet
	}{
		{
			"empty record",
			map[string]interface{}{},
			map[string]interface{}{},
			model.LabelSet{},
		},
		{
			"empty subrecord",
			map[string]interface{}{
				"kubernetes": map[interface{}]interface{}{
					"foo": []byte("buzz"),
				},
			},
			map[string]interface{}{},
			model.LabelSet{},
		},
		{
			"deep string",
			map[string]interface{}{
				"int":   "42",
				"float": "42.42",
				"array": `[42,42.42,"foo"]`,
				"kubernetes": map[string]interface{}{
					"label": map[string]interface{}{
						"component": map[string]interface{}{
							"buzz": "value",
						},
					},
				},
			},
			map[string]interface{}{
				"int":   "int",
				"float": "float",
				"array": "array",
				"kubernetes": map[string]interface{}{
					"label": map[string]interface{}{
						"component": map[string]interface{}{
							"buzz": "label",
						},
					},
				},
				"stream": "output",
				"nope":   "nope",
			},
			model.LabelSet{
				"int":   "42",
				"float": "42.42",
				"array": `[42,42.42,"foo"]`,
				"label": "value",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.LabelSet{}
			if mapLabels(tt.records, tt.mapping, got); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mapLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}
