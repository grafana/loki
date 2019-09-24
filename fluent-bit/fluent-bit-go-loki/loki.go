package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/promtail/client"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/logging"
)

type loki struct {
	cfg    *config
	client client.Client
	logger log.Logger
}

func newPlugin(cfg *config, logger log.Logger) (*loki, error) {
	client, err := client.New(cfg.clientConfig, logger)
	if err != nil {
		return nil, err
	}
	return &loki{
		cfg:    cfg,
		client: client,
		logger: logger,
	}, nil
}

// sendRecord send fluentbit records to loki as an entry.
func (l *loki) sendRecord(r map[interface{}]interface{}, ts time.Time) error {
	records := toStringMap(r)
	level.Debug(l.logger).Log("msg", "processing records", "records", fmt.Sprintf("%v", records))
	lbs := extractLabels(records, l.cfg.labelKeys)
	removeKeys(records, append(l.cfg.labelKeys, l.cfg.removeKeys...))
	if len(records) == 0 {
		return nil
	}
	if l.cfg.dropSingleKey && len(records) == 1 {
		for _, v := range records {
			return l.client.Handle(lbs, ts, fmt.Sprintf("%v", v))
		}
	}
	line, err := createLine(records, l.cfg.lineFormat)
	if err != nil {
		level.Error(l.logger).Log("msg", "error creating line", "error", err)
		return nil
	}
	return l.client.Handle(lbs, ts, line)
}

func toStringMap(record map[interface{}]interface{}) map[string]interface{} {
	m := make(map[string]interface{})

	for k, v := range record {
		key, ok := k.(string)
		if !ok {
			continue
		}
		switch t := v.(type) {
		case []byte:
			// prevent encoding to base64
			m[key] = string(t)
		default:
			m[key] = v
		}
	}
	return m
}

func extractLabels(records map[string]interface{}, keys []string) model.LabelSet {
	// right now we extract labels only from the outer most keys.
	// in the future we should allow to extract `foo.bar.x`
	res := model.LabelSet{}
	for _, k := range keys {
		v, ok := records[k]
		if !ok {
			continue
		}
		ln := model.LabelName(k)
		// skips invalid name and values
		if !ln.IsValid() {
			continue
		}
		lv := model.LabelValue(fmt.Sprintf("%v", v))
		if !lv.IsValid() {
			continue
		}
		res[ln] = lv
	}
	return res
}

func removeKeys(records map[string]interface{}, keys []string) {
	for _, k := range keys {
		delete(records, k)
	}
}

func createLine(records map[string]interface{}, f format) (string, error) {
	switch f {
	case jsonFormat:
		js, err := jsoniter.Marshal(records)
		if err != nil {
			return "", err
		}
		return string(js), nil
	case kvPairFormat:
		buff := &bytes.Buffer{}
		var keys []string
		for k := range records {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, err := fmt.Fprintf(buff, "%s=%v ", k, records[k])
			if err != nil {
				return "", err
			}
		}
		res := buff.String()
		if len(records) > 0 {
			return res[:len(res)-1], nil
		}
		return res, nil
	default:
		return "", fmt.Errorf("invalid line format: %v", f)
	}
}

func newLogger(logLevel logging.Level) log.Logger {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = level.NewFilter(logger, logLevel.Gokit)
	logger = log.With(logger, "caller", log.Caller(3))
	return logger
}
