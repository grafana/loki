package main

import (
	"encoding/json"
	"testing"
	"time"
	"unsafe"

	"github.com/fluent/fluent-bit-go/output"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func TestCreateJSON(t *testing.T) {
	record := make(map[interface{}]interface{})
	record["key"] = "value"
	record["number"] = 8

	line, err := createJSON(record)
	if err != nil {
		assert.Fail(t, "createJSON fails:%v", err)
	}
	assert.NotNil(t, line, "json string not to be nil")
	result := make(map[string]interface{})
	jsonBytes := ([]byte)(line)
	err = json.Unmarshal(jsonBytes, &result)
	if err != nil {
		assert.Fail(t, "unmarshal of json fails:%v", err)
	}

	assert.Equal(t, result["key"], "value")
	assert.Equal(t, result["number"], float64(8))
}

type testrecord struct {
	rc   int
	ts   interface{}
	data map[interface{}]interface{}
}

type events struct {
	data []byte
}
type testFluentPlugin struct {
	url       string
	batchWait string
	batchSize string
	records   []testrecord
	position  int
	events    []*events
}

func (p *testFluentPlugin) PluginConfigKey(ctx unsafe.Pointer, key string) string {
	switch key {
	case "URL":
		return p.url
	case "BatchWait":
		return p.batchWait
	case "BatchSize":
		return p.batchSize
	case "Labels":
		return `
{"labels": [{"key": "job", "label": "fluent-bit"}]}
`
	}
	return "unknown-" + key
}

func (p *testFluentPlugin) Unregister(ctx unsafe.Pointer) {}
func (p *testFluentPlugin) GetRecord(dec *output.FLBDecoder) (int, interface{}, map[interface{}]interface{}) {
	if p.position < len(p.records) {
		r := p.records[p.position]
		p.position++
		return r.rc, r.ts, r.data
	}
	return -1, nil, nil
}
func (p *testFluentPlugin) NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder { return nil }
func (p *testFluentPlugin) Exit(code int)                                                 {}
func (p *testFluentPlugin) HandleLine(ls model.LabelSet, timestamp time.Time, line string) error {
	data := ([]byte)(line)
	events := &events{data: data}
	p.events = append(p.events, events)
	return nil
}
func (p *testFluentPlugin) addrecord(rc int, ts interface{}, line map[interface{}]interface{}) {
	p.records = append(p.records, testrecord{rc: rc, ts: ts, data: line})
}

func TestPluginInitialization(t *testing.T) {
	plugin = &testFluentPlugin{url: "http://localhost:3100/api/prom/push"}
	res := FLBPluginInit(nil)
	assert.Equal(t, output.FLB_OK, res)
}

func TestPluginFlusher(t *testing.T) {
	testplugin := &testFluentPlugin{url: "http://localhost:3100/api/prom/push"}
	ts := time.Date(2019, time.March, 10, 10, 11, 12, 0, time.UTC)
	testrecords := map[interface{}]interface{}{
		"mykey": "myvalue",
	}
	testplugin.addrecord(0, output.FLBTime{Time: ts}, testrecords)
	testplugin.addrecord(0, uint64(ts.Unix()), testrecords)
	testplugin.addrecord(0, 0, testrecords)
	plugin = testplugin
	res := FLBPluginFlush(nil, 0, nil)
	assert.Equal(t, output.FLB_OK, res)
	assert.Len(t, testplugin.events, len(testplugin.records))
	var parsed map[string]interface{}
	err := json.Unmarshal(testplugin.events[0].data, &parsed)
	if err != nil {
		assert.Fail(t, "unmarshal of json fails:%v", err)
	}
	assert.Equal(t, testrecords["mykey"], parsed["mykey"])
	err = json.Unmarshal(testplugin.events[1].data, &parsed)
	if err != nil {
		assert.Fail(t, "unmarshal of json fails:%v", err)
	}
	err = json.Unmarshal(testplugin.events[2].data, &parsed)
	if err != nil {
		assert.Fail(t, "unmarshal of json fails:%v", err)
	}
}
