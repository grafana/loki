package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/go-logfmt/logfmt"
	dslog "github.com/grafana/dskit/log"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client"

	"github.com/grafana/loki/v3/pkg/logproto"
)

var (
	lineReplacer = strings.NewReplacer(`\n`, "\n", `\t`, "\t")
	keyReplacer  = strings.NewReplacer("/", "_", ".", "_", "-", "_")
)

type loki struct {
	cfg    *config
	client client.Client
	logger log.Logger
}

func newPlugin(cfg *config, logger log.Logger, metrics *client.Metrics) (*loki, error) {
	client, err := NewClient(cfg, logger, metrics)
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
			l.client.Chan() <- api.Entry{
				Labels: lbs,
				Entry: logproto.Entry{
					Timestamp: ts,
					Line:      fmt.Sprintf("%v", v),
				},
			}
			return nil
		}
	}
	line, err := l.createLine(records, l.cfg.lineFormat)
	if err != nil {
		return fmt.Errorf("error creating line: %v", err)
	}
	l.client.Chan() <- api.Entry{
		Labels: lbs,
		Entry: logproto.Entry{
			Timestamp: ts,
			Line:      line,
		},
	}
	return nil
}

// prevent base64-encoding []byte values (default json.Encoder rule) by
// converting them to strings
func toStringSlice(slice []interface{}) []interface{} {
	var s []interface{}
	for _, v := range slice {
		switch t := v.(type) {
		case []byte:
			s = append(s, string(t))
		case map[interface{}]interface{}:
			s = append(s, toStringMap(t))
		case []interface{}:
			s = append(s, toStringSlice(t))
		default:
			s = append(s, t)
		}
	}
	return s
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
			m[key] = string(t)
		case map[interface{}]interface{}:
			m[key] = toStringMap(t)
		case []interface{}:
			m[key] = toStringSlice(t)
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

	for k, v := range kube.(map[string]interface{}) {
		switch k {
		case "labels":
			for m, n := range v.(map[string]interface{}) {
				kuberneteslbs[model.LabelName(keyReplacer.Replace(m))] = model.LabelValue(fmt.Sprintf("%v", n))
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
	if len(keys) == 0 {
		return model.LabelSet{}
	}

	res := make(model.LabelSet, len(keys)) // Pre-allocate with expected size
	for _, k := range keys {
		v, ok := getNestedValue(records, k)
		if !ok {
			continue
		}

		// Convert dot notation to valid label name by replacing dots with underscores
		// Only allocate new string if dots are present
		var labelName string
		if strings.Contains(k, ".") {
			labelName = strings.ReplaceAll(k, ".", "_")
		} else {
			labelName = k
		}

		ln := model.LabelName(labelName)

		// skips invalid name and values
		if !model.LegacyValidation.IsValidLabelName(string(ln)) {
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

// traverseNestedMap navigates through nested maps using dot notation
// Returns the final map and key for operations, or just the value if getValue is true
func traverseNestedMap(records map[string]interface{}, dottedKey string, getValue bool) (interface{}, map[string]interface{}, string, bool) {
	// Input validation
	if records == nil || dottedKey == "" {
		return nil, nil, "", false
	}

	if !strings.Contains(dottedKey, ".") {
		// Handle non-nested keys directly
		if getValue {
			if value, exists := records[dottedKey]; exists {
				return value, nil, "", true
			}
		}
		return nil, records, dottedKey, true
	}

	keys := strings.Split(dottedKey, ".")

	// Filter out empty segments (e.g., from "a..b" or ".a.b")
	validKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if key != "" {
			validKeys = append(validKeys, key)
		}
	}

	if len(validKeys) == 0 {
		return nil, nil, "", false
	}

	if len(validKeys) == 1 {
		// After filtering, it's just a simple key
		key := validKeys[0]
		if getValue {
			if value, exists := records[key]; exists {
				return value, nil, "", true
			}
			return nil, nil, "", false
		}
		return nil, records, key, true
	}

	current := records

	// Navigate to the target location
	for i := 0; i < len(validKeys)-1; i++ {
		value, exists := current[validKeys[i]]
		if !exists {
			return nil, nil, "", false
		}

		nextMap, ok := value.(map[string]interface{})
		if !ok {
			return nil, nil, "", false
		}
		current = nextMap
	}

	finalKey := validKeys[len(validKeys)-1]

	if getValue {
		if value, exists := current[finalKey]; exists {
			return value, current, finalKey, true
		}
		return nil, nil, "", false
	}

	return nil, current, finalKey, true
}

// getNestedValue retrieves a value from a nested map using dot notation
// For example, "log.level" will traverse records["log"]["level"]
func getNestedValue(records map[string]interface{}, dottedKey string) (interface{}, bool) {
	// Fast path for simple keys
	if !strings.Contains(dottedKey, ".") {
		value, exists := records[dottedKey]
		return value, exists
	}

	value, _, _, ok := traverseNestedMap(records, dottedKey, true)
	return value, ok
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
				if lValue.IsValid() && model.UTF8Validation.IsValidLabelName(string(lName)) {
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
		removeNestedKey(records, k)
	}
}

// removeNestedKey removes a key from the records map, supporting both regular and dot notation
func removeNestedKey(records map[string]interface{}, dottedKey string) {
	_, parentMap, finalKey, ok := traverseNestedMap(records, dottedKey, false)
	if ok && parentMap != nil {
		delete(parentMap, finalKey)
	}
}

func (l *loki) createLine(records map[string]interface{}, f format) (string, error) {
	switch f {
	case jsonFormat:
		for k, v := range records {
			if s, ok := v.(string); ok && (strings.Contains(s, "{") || strings.Contains(s, "[")) {
				var data interface{}
				err := json.Unmarshal([]byte(s), &data)
				if err != nil {
					// keep this debug as it can be very verbose
					level.Debug(l.logger).Log("msg", "error unmarshalling json", "err", err)
					continue
				}
				records[k] = data
			}
		}
		js, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(records)
		if err != nil {
			return "", err
		}
		return string(js), nil
	case kvPairFormat:
		buf := &bytes.Buffer{}
		enc := logfmt.NewEncoder(buf)
		keys := make([]string, 0, len(records))
		for k := range records {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			err := enc.EncodeKeyval(k, records[k])
			if err == logfmt.ErrUnsupportedValueType {
				err := enc.EncodeKeyval(k, fmt.Sprintf("%+v", records[k]))
				if err != nil {
					return "", nil
				}
				continue
			}
			if err != nil {
				return "", nil
			}
		}
		return lineReplacer.Replace(buf.String()), nil
	default:
		return "", fmt.Errorf("invalid line format: %v", f)
	}
}

func newLogger(logLevel dslog.Level) log.Logger {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = level.NewFilter(logger, logLevel.Option)
	logger = log.With(logger, "caller", log.Caller(3))
	return logger
}
