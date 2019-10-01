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
	var recordFixture = map[interface{}]interface{}{
		"foo":   "bar",
		"bar":   500,
		"error": make(chan struct{}),
	}

	tests := []struct {
		name string
		cfg  *config
		want *entry
	}{
		{"not enough records", &config{labelKeys: []string{"foo"}, lineFormat: jsonFormat, removeKeys: []string{"bar", "error"}}, nil},
		{"labels", &config{labelKeys: []string{"bar", "fake"}, lineFormat: jsonFormat, removeKeys: []string{"fuzz", "error"}}, &entry{model.LabelSet{"bar": "500"}, `{"foo":"bar"}`, now}},
		{"remove key", &config{labelKeys: []string{"fake"}, lineFormat: jsonFormat, removeKeys: []string{"foo", "error", "fake"}}, &entry{model.LabelSet{}, `{"bar":500}`, now}},
		{"error", &config{labelKeys: []string{"fake"}, lineFormat: jsonFormat, removeKeys: []string{"foo"}}, nil},
		{"key value", &config{labelKeys: []string{"fake"}, lineFormat: kvPairFormat, removeKeys: []string{"foo", "error", "fake"}}, &entry{model.LabelSet{}, `bar=500`, now}},
		{"single", &config{labelKeys: []string{"fake"}, dropSingleKey: true, lineFormat: kvPairFormat, removeKeys: []string{"foo", "error", "fake"}}, &entry{model.LabelSet{}, `500`, now}},
		{"labelmap", &config{labeMap: map[string]interface{}{"bar": "other"}, lineFormat: jsonFormat, removeKeys: []string{"bar", "error"}}, &entry{model.LabelSet{"other": "500"}, `{"foo":"bar"}`, now}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			l := &loki{
				cfg:    tt.cfg,
				client: rec,
				logger: logger,
			}
			_ = l.sendRecord(recordFixture, now)
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
		{"bad json", map[string]interface{}{"foo": make(chan interface{})}, jsonFormat, "", true},
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
			"bytes string",
			map[string]interface{}{
				"kubernetes": map[interface{}]interface{}{
					"foo": []byte("buzz"),
				},
				"stream": "stderr",
			},
			map[string]interface{}{
				"kubernetes": map[string]interface{}{
					"foo":         "test",
					"nonexisting": "",
				},
				"stream": "output",
				"nope":   "nope",
			},
			model.LabelSet{"test": "buzz", "output": "stderr"},
		},
		{
			"numeric label",
			map[string]interface{}{
				"kubernetes": map[interface{}]interface{}{
					"integer":        42,
					"floating_point": 42.42,
				},
				"stream": "stderr",
			},
			map[string]interface{}{
				"kubernetes": map[string]interface{}{
					"integer":        "integer",
					"floating_point": "floating_point",
				},
				"stream": "output",
				"nope":   "nope",
			},
			model.LabelSet{"integer": "42", "floating_point": "42.42", "output": "stderr"},
		},
		{
			"list label",
			map[string]interface{}{
				"kubernetes": map[interface{}]interface{}{
					"integers": []int{42, 43},
				},
			},
			map[string]interface{}{
				"kubernetes": map[string]interface{}{
					"integers": "integers",
				},
			},
			model.LabelSet{"integers": "[42 43]"},
		},
		{
			"deep string",
			map[string]interface{}{
				"kubernetes": map[interface{}]interface{}{
					"label": map[interface{}]interface{}{
						"component": map[interface{}]interface{}{
							"buzz": "value",
						},
					},
				},
			},
			map[string]interface{}{
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
			model.LabelSet{"label": "value"},
		},
		{
			"skip invalid values",
			map[string]interface{}{
				"kubernetes": map[interface{}]interface{}{
					"annotations": map[interface{}]interface{}{
						"kubernetes.io/config.source": "cfg",
						"kubernetes.io/config.hash":   []byte("a\xc5z"),
					},
					"container_name": "loki",
					"namespace_name": "dev",
					"pod_name":       "loki-asdwe",
				},
			},
			map[string]interface{}{
				"kubernetes": map[string]interface{}{
					"annotations": map[string]interface{}{
						"kubernetes.io/config.source": "source",
						"kubernetes.io/config.hash":   "hash",
					},
					"container_name": "container",
					"namespace_name": "namespace",
					"pod_name":       "instance",
				},
				"stream": "output",
			},
			model.LabelSet{"container": "loki", "instance": "loki-asdwe", "namespace": "dev", "source": "cfg"},
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
