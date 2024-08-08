package main

import (
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/log"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client"

	lokiflag "github.com/grafana/loki/v3/pkg/util/flagext"
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
					Timeout:        defaultClientCfg.Timeout,
					ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"job": "fluent-bit"}},
					BackoffConfig:  defaultClientCfg.BackoffConfig,
				},
				logLevel:      mustParseLogLevel("info"),
				dropSingleKey: true,
			},
			false},
		{"setting values",
			map[string]string{
				"URL":           "http://somewhere.com:3100/loki/api/v1/push",
				"TenantID":      "my-tenant-id",
				"LineFormat":    "key_value",
				"LogLevel":      "warn",
				"Labels":        `{app="foo"}`,
				"BatchSize":     "100",
				"BatchWait":     "30",
				"Timeout":       "1s",
				"RemoveKeys":    "buzz,fuzz",
				"LabelKeys":     "foo,bar",
				"DropSingleKey": "false",
				"MinBackoff":    "1ms",
				"MaxBackoff":    "5m",
				"MaxRetries":    "10",
			},
			&config{
				lineFormat: kvPairFormat,
				clientConfig: client.Config{
					URL:            mustParseURL("http://somewhere.com:3100/loki/api/v1/push"),
					TenantID:       "my-tenant-id",
					BatchSize:      100,
					BatchWait:      mustParseDuration("30s"),
					Timeout:        mustParseDuration("1s"),
					ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"app": "foo"}},
					BackoffConfig:  backoff.Config{MinBackoff: mustParseDuration("1ms"), MaxBackoff: mustParseDuration("5m"), MaxRetries: 10},
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
				"BatchSize":     "100",
				"BatchWait":     "30s",
				"Timeout":       "1s",
				"RemoveKeys":    "buzz,fuzz",
				"LabelKeys":     "foo,bar",
				"DropSingleKey": "false",
				"MinBackoff":    "1ms",
				"MaxBackoff":    "5m",
				"MaxRetries":    "10",
				"LabelMapPath":  fileName,
			},
			&config{
				lineFormat: kvPairFormat,
				clientConfig: client.Config{
					URL:            mustParseURL("http://somewhere.com:3100/loki/api/v1/push"),
					TenantID:       "", // empty as not set in fluent-bit plugin config map
					BatchSize:      100,
					BatchWait:      mustParseDuration("30s"),
					Timeout:        mustParseDuration("1s"),
					ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"app": "foo"}},
					BackoffConfig:  backoff.Config{MinBackoff: mustParseDuration("1ms"), MaxBackoff: mustParseDuration("5m"), MaxRetries: 10},
				},
				logLevel:      mustParseLogLevel("warn"),
				labelKeys:     nil,
				removeKeys:    []string{"buzz", "fuzz"},
				dropSingleKey: false,
				labelMap: map[string]interface{}{
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
		{"bad BatchWait", map[string]string{"BatchWait": "30sa"}, nil, true},
		{"bad BatchSize", map[string]string{"BatchSize": "a"}, nil, true},
		{"bad Timeout", map[string]string{"Timeout": "1a"}, nil, true},
		{"bad labels", map[string]string{"Labels": "a"}, nil, true},
		{"bad format", map[string]string{"LineFormat": "a"}, nil, true},
		{"bad log level", map[string]string{"LogLevel": "a"}, nil, true},
		{"bad drop single key", map[string]string{"DropSingleKey": "a"}, nil, true},
		{"bad MinBackoff", map[string]string{"MinBackoff": "1msa"}, nil, true},
		{"bad MaxBackoff", map[string]string{"MaxBackoff": "5ma"}, nil, true},
		{"bad MaxRetries", map[string]string{"MaxRetries": "a"}, nil, true},
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
	if expected.clientConfig.Timeout != actual.clientConfig.Timeout {
		t.Errorf("incorrect Timeout want:%v got:%v", expected.clientConfig.Timeout, actual.clientConfig.Timeout)
	}
	if expected.clientConfig.BackoffConfig.MinBackoff != actual.clientConfig.BackoffConfig.MinBackoff {
		t.Errorf("incorrect MinBackoff want:%v got:%v", expected.clientConfig.BackoffConfig.MinBackoff, actual.clientConfig.BackoffConfig.MinBackoff)
	}
	if expected.clientConfig.BackoffConfig.MaxBackoff != actual.clientConfig.BackoffConfig.MaxBackoff {
		t.Errorf("incorrect MaxBackoff want:%v got:%v", expected.clientConfig.BackoffConfig.MaxBackoff, actual.clientConfig.BackoffConfig.MaxBackoff)
	}
	if expected.clientConfig.BackoffConfig.MaxRetries != actual.clientConfig.BackoffConfig.MaxRetries {
		t.Errorf("incorrect MaxRetries want:%v got:%v", expected.clientConfig.BackoffConfig.MaxRetries, actual.clientConfig.BackoffConfig.MaxRetries)
	}
	if !reflect.DeepEqual(expected.clientConfig.URL, actual.clientConfig.URL) {
		t.Errorf("incorrect URL want:%v got:%v", expected.clientConfig.URL, actual.clientConfig.URL)
	}
	if !reflect.DeepEqual(expected.clientConfig.TenantID, actual.clientConfig.TenantID) {
		t.Errorf("incorrect TenantID want:%v got:%v", expected.clientConfig.TenantID, actual.clientConfig.TenantID)
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
	if !reflect.DeepEqual(expected.labelMap, actual.labelMap) {
		t.Errorf("incorrect labelMap want:%v got:%v", expected.labelMap, actual.labelMap)
	}
}

func mustParseURL(u string) flagext.URLValue {
	parsed, err := url.Parse(u)
	if err != nil {
		panic(err)
	}
	return flagext.URLValue{URL: parsed}
}

func mustParseLogLevel(l string) log.Level {
	level := log.Level{}
	err := level.Set(l)
	if err != nil {
		panic(err)
	}
	return level
}

func mustParseDuration(u string) time.Duration {
	parsed, err := time.ParseDuration(u)
	if err != nil {
		panic(err)
	}
	return parsed
}

func createTempLabelMap(t *testing.T) string {
	file, err := os.CreateTemp("", "labelmap")
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
