package main

import (
	"io/ioutil"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/grafana/loki/pkg/promtail/client"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
	"github.com/weaveworks/common/logging"
)

type fakeConfig map[string]string

func (f fakeConfig) Get(key string) string {
	return f[key]
}

func Test_parseConfig(t *testing.T) {
	fileName := createTempLabelMap(t)
	defer os.Remove(fileName)

	tests := []struct {
		name    string
		conf    map[string]string
		want    *config
		wantErr bool
	}{
		{"defaults",
			map[string]string{},
			&config{
				lineFormat: jsonFormat,
				clientConfig: client.Config{
					URL:            mustParseURL("http://localhost:3100/loki/api/v1/push"),
					BatchSize:      defaultClientCfg.BatchSize,
					BatchWait:      defaultClientCfg.BatchWait,
					ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"job": "fluent-bit"}},
				},
				logLevel:      mustParseLogLevel("info"),
				dropSingleKey: true,
			},
			false},
		{"setting values",
			map[string]string{
				"URL":           "http://somewhere.com:3100/loki/api/v1/push",
				"LineFormat":    "key_value",
				"LogLevel":      "warn",
				"Labels":        `{app="foo"}`,
				"BatchWait":     "30",
				"BatchSize":     "100",
				"RemoveKeys":    "buzz,fuzz",
				"LabelKeys":     "foo,bar",
				"DropSingleKey": "false",
			},
			&config{
				lineFormat: kvPairFormat,
				clientConfig: client.Config{
					URL:            mustParseURL("http://somewhere.com:3100/loki/api/v1/push"),
					BatchSize:      100,
					BatchWait:      30 * time.Second,
					ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"app": "foo"}},
				},
				logLevel:      mustParseLogLevel("warn"),
				labelKeys:     []string{"foo", "bar"},
				removeKeys:    []string{"buzz", "fuzz"},
				dropSingleKey: false,
			},
			false},
		{"with label map",
			map[string]string{
				"URL":           "http://somewhere.com:3100/loki/api/v1/push",
				"LineFormat":    "key_value",
				"LogLevel":      "warn",
				"Labels":        `{app="foo"}`,
				"BatchWait":     "30",
				"BatchSize":     "100",
				"RemoveKeys":    "buzz,fuzz",
				"LabelKeys":     "foo,bar",
				"DropSingleKey": "false",
				"LabelMapPath":  fileName,
			},
			&config{
				lineFormat: kvPairFormat,
				clientConfig: client.Config{
					URL:            mustParseURL("http://somewhere.com:3100/loki/api/v1/push"),
					BatchSize:      100,
					BatchWait:      30 * time.Second,
					ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"app": "foo"}},
				},
				logLevel:      mustParseLogLevel("warn"),
				labelKeys:     nil,
				removeKeys:    []string{"buzz", "fuzz"},
				dropSingleKey: false,
				labeMap: map[string]interface{}{
					"kubernetes": map[string]interface{}{
						"container_name": "container",
						"host":           "host",
						"namespace_name": "namespace",
						"pod_name":       "instance",
						"labels": map[string]interface{}{
							"component": "component",
							"tier":      "tier",
						},
					},
					"stream": "stream",
				},
			},
			false},
		{"bad url", map[string]string{"URL": "::doh.com"}, nil, true},
		{"bad BatchWait", map[string]string{"BatchWait": "a"}, nil, true},
		{"bad BatchSize", map[string]string{"BatchSize": "a"}, nil, true},
		{"bad labels", map[string]string{"Labels": "a"}, nil, true},
		{"bad format", map[string]string{"LineFormat": "a"}, nil, true},
		{"bad log level", map[string]string{"LogLevel": "a"}, nil, true},
		{"bad drop single key", map[string]string{"DropSingleKey": "a"}, nil, true},
		{"bad labelmap file", map[string]string{"LabelMapPath": "a"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConfig(fakeConfig(tt.conf))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				assertConfig(t, got, tt.want)
			}
		})
	}
}

func assertConfig(t *testing.T, expected, actual *config) {
	if expected.clientConfig.BatchSize != actual.clientConfig.BatchSize {
		t.Errorf("incorrect batch size want:%v got:%v", expected.clientConfig.BatchSize, actual.clientConfig.BatchSize)
	}
	if !reflect.DeepEqual(expected.clientConfig.ExternalLabels, actual.clientConfig.ExternalLabels) {
		t.Errorf("incorrect labels want:%v got:%v", expected.clientConfig.ExternalLabels, actual.clientConfig.ExternalLabels)
	}
	if expected.clientConfig.BatchWait != actual.clientConfig.BatchWait {
		t.Errorf("incorrect batch wait want:%v got:%v", expected.clientConfig.BatchWait, actual.clientConfig.BatchWait)
	}
	if !reflect.DeepEqual(expected.clientConfig.URL, actual.clientConfig.URL) {
		t.Errorf("incorrect URL want:%v got:%v", expected.clientConfig.URL, actual.clientConfig.URL)
	}
	if !reflect.DeepEqual(expected.lineFormat, actual.lineFormat) {
		t.Errorf("incorrect lineFormat want:%v got:%v", expected.lineFormat, actual.lineFormat)
	}
	if !reflect.DeepEqual(expected.removeKeys, actual.removeKeys) {
		t.Errorf("incorrect removeKeys want:%v got:%v", expected.removeKeys, actual.removeKeys)
	}
	if !reflect.DeepEqual(expected.labelKeys, actual.labelKeys) {
		t.Errorf("incorrect labelKeys want:%v got:%v", expected.labelKeys, actual.labelKeys)
	}
	if expected.logLevel.String() != actual.logLevel.String() {
		t.Errorf("incorrect logLevel want:%v got:%v", expected.logLevel.String(), actual.logLevel.String())
	}
	if !reflect.DeepEqual(expected.labeMap, actual.labeMap) {
		t.Errorf("incorrect labeMap want:%v got:%v", expected.labeMap, actual.labeMap)
	}
}

func mustParseURL(u string) flagext.URLValue {
	parsed, err := url.Parse(u)
	if err != nil {
		panic(err)
	}
	return flagext.URLValue{URL: parsed}
}

func mustParseLogLevel(l string) logging.Level {
	level := logging.Level{}
	err := level.Set(l)
	if err != nil {
		panic(err)
	}
	return level
}

func createTempLabelMap(t *testing.T) string {
	file, err := ioutil.TempFile("", "labelmap")
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(`{
        "kubernetes": {
            "namespace_name": "namespace",
            "labels": {
                "component": "component",
                "tier": "tier"
            },
            "host": "host",
            "container_name": "container",
            "pod_name": "instance"
        },
        "stream": "stream"
    }`)
	if err != nil {
		t.Fatal(err)
	}
	return file.Name()
}
