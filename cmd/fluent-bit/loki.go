package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/logging"

	"github.com/grafana/loki/pkg/promtail/client"
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
	level.Debug(l.logger).Log("msg", "processing records", "records", fmt.Sprintf("%+v", records))
	lbs := model.LabelSet{}
	if l.cfg.autoKubernetesLabels {
		err := autoLabels(records, lbs)
		if err != nil {
			level.Error(l.logger).Log("msg", err.Error(), "records", fmt.Sprintf("%+v", records))
		}
	} else if l.cfg.labelMap != nil {
		mapLabels(records, l.cfg.labelMap, lbs)
	} else {
		lbs = extractLabels(records, l.cfg.labelKeys)
	}
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
		return fmt.Errorf("error creating line: %v", err)
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
		case map[interface{}]interface{}:
			m[key] = toStringMap(t)
		default:
			m[key] = v
		}
	}

	return m
}

func autoLabels(records map[string]interface{}, kuberneteslbs model.LabelSet) error {
	kube, ok := records["kubernetes"]
	if !ok {
		return errors.New("kubernetes labels not found, no labels will be added")
	}

	replacer := strings.NewReplacer("/", "_", ".", "_", "-", "_")
	for k, v := range kube.(map[string]interface{}) {
		switch k {
		case "labels":
			for m, n := range v.(map[string]interface{}) {
				kuberneteslbs[model.LabelName(replacer.Replace(m))] = model.LabelValue(fmt.Sprintf("%v", n))
			}
		case "docker_id", "pod_id", "annotations":
			// do nothing
			continue
		default:
			kuberneteslbs[model.LabelName(k)] = model.LabelValue(fmt.Sprintf("%v", v))
		}
	}

	return nil
}

func extractLabels(records map[string]interface{}, keys []string) model.LabelSet {
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

// mapLabels convert records into labels using a json map[string]interface{} mapping
func mapLabels(records map[string]interface{}, mapping map[string]interface{}, res model.LabelSet) {
	for k, v := range mapping {
		switch nextKey := v.(type) {
		// if the next level is a map we are expecting we need to move deeper in the tree
		case map[string]interface{}:
			if nextValue, ok := records[k].(map[string]interface{}); ok {
				// recursively search through the next level map.
				mapLabels(nextValue, nextKey, res)
			}
		// we found a value in the mapping meaning we need to save the corresponding record value for the given key.
		case string:
			if value, ok := getRecordValue(k, records); ok {
				lName := model.LabelName(nextKey)
				lValue := model.LabelValue(value)
				if lValue.IsValid() && lName.IsValid() {
					res[lName] = lValue
				}
			}
		}
	}
}

func getRecordValue(key string, records map[string]interface{}) (string, bool) {
	if value, ok := records[key]; ok {
		switch typedVal := value.(type) {
		case string:
			return typedVal, true
		case []byte:
			return string(typedVal), true
		default:
			return fmt.Sprintf("%v", typedVal), true
		}
	}
	return "", false
}

func removeKeys(records map[string]interface{}, keys []string) {
	for _, k := range keys {
		delete(records, k)
	}
}

func createLine(records map[string]interface{}, f format) (string, error) {
	switch f {
	case jsonFormat:
		js, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(records)
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
