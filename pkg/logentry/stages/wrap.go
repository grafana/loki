package stages

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	json "github.com/json-iterator/go"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

const (
	entryKey = "_entry"
)

var (
	reallyTrue  = true
	reallyFalse = false
)

type Wrapped struct {
	Labels map[string]string `json:",inline"`
	Entry  string            `json:"_entry"`
}

// UnmarshalJSON populates a Wrapped struct where every key except the _entry key is added to the Labels field
func (w *Wrapped) UnmarshalJSON(data []byte) error {
	m := &map[string]interface{}{}
	err := json.Unmarshal(data, m)
	if err != nil {
		return err
	}
	w.Labels = map[string]string{}
	for k, v := range *m {
		// _entry key goes to the Entry field, everything else becomes a label
		if k == entryKey {
			if s, ok := v.(string); ok {
				w.Entry = s
			} else {
				return errors.New("failed to unmarshal json, all values must be of type string")
			}
		} else {
			if s, ok := v.(string); ok {
				w.Labels[k] = s
			} else {
				return errors.New("failed to unmarshal json, all values must be of type string")
			}
		}
	}
	return nil
}

// MarshalJSON creates a Wrapped struct as JSON where the Labels are flattened into the top level of the object
func (w Wrapped) MarshalJSON() ([]byte, error) {

	// Marshal the entry to properly escape if it's json or contains quotes
	b, err := json.Marshal(w.Entry)
	if err != nil {
		return nil, err
	}

	// Creating a map and marshalling from a map results in a non deterministic ordering of the resulting json object
	// This is functionally ok but really annoying to humans and automated tests.
	// Instead we will build the json ourselves after sorting all the labels to get a consistent output
	keys := make([]string, 0, len(w.Labels))
	for k := range w.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer

	buf.WriteString("{")
	for i, k := range keys {
		if i != 0 {
			buf.WriteString(",")
		}
		// marshal key
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteString(":")
		// marshal value
		val, err := json.Marshal(w.Labels[k])
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}
	// Only add the comma if something exists in the buffer other than "{"
	if buf.Len() > 1 {
		buf.WriteString(",")
	}
	// Add the line entry
	buf.WriteString("\"" + entryKey + "\":")
	buf.Write(b)

	buf.WriteString("}")
	return buf.Bytes(), nil
}

// WrapConfig contains the configuration for a wrapStage
type WrapConfig struct {
	Labels          []string `mapstrcuture:"labels"`
	IngestTimestamp *bool    `mapstructure:"ingest_timestamp"`
}

//nolint:unparam // Always returns nil until someone adds more validation and can remove this.
// validateWrapConfig validates the WrapConfig for the wrapStage
func validateWrapConfig(cfg *WrapConfig) error {
	// Default the IngestTimestamp value to be true
	if cfg.IngestTimestamp == nil {
		cfg.IngestTimestamp = &reallyTrue
	}
	return nil
}

// newWrapStage creates a DropStage from config
func newWrapStage(logger log.Logger, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &WrapConfig{}
	err := mapstructure.WeakDecode(config, cfg)
	if err != nil {
		return nil, err
	}
	err = validateWrapConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &wrapStage{
		logger:    log.With(logger, "component", "stage", "type", "wrap"),
		cfg:       cfg,
		dropCount: getDropCountMetric(registerer),
	}, nil
}

// wrapStage applies Label matchers to determine if the include stages should be run
type wrapStage struct {
	logger    log.Logger
	cfg       *WrapConfig
	dropCount *prometheus.CounterVec
}

func (m *wrapStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			out <- m.wrap(e)
		}
	}()
	return out
}

func (m *wrapStage) wrap(e Entry) Entry {
	lbls := e.Labels
	wrappedLabels := make(map[string]string, len(m.cfg.Labels))
	foundLables := []model.LabelName{}

	// Iterate through all the extracted map (which also includes all the labels)
	for lk, lv := range e.Extracted {
		for _, wl := range m.cfg.Labels {
			if lk == wl {
				sv, err := getString(lv)
				if err != nil {
					if Debug {
						level.Debug(m.logger).Log("msg", fmt.Sprintf("value for key: '%s' cannot be converted to a string and cannot be wrapped", lk), "err", err, "type", reflect.TypeOf(lv))
					}
					continue
				}
				wrappedLabels[wl] = sv
				foundLables = append(foundLables, model.LabelName(lk))
			}
		}
	}

	// Embed the extracted labels into the wrapper object
	w := Wrapped{
		Labels: wrappedLabels,
		Entry:  e.Line,
	}

	// Marshal to json
	wl, err := json.Marshal(w)
	if err != nil {
		if Debug {
			level.Debug(m.logger).Log("msg", "wrap stage failed to marshal wrapped object to json, wrapping will be skipped", "err", err)
		}
		return e
	}

	// Remove anything found which is also a label, do this after the marshalling to not remove labels until
	// we are sure the line can be successfully wrapped.
	for _, fl := range foundLables {
		delete(lbls, fl)
	}

	// Replace the labels and the line with new values
	e.Labels = lbls
	e.Line = string(wl)

	// If the config says to re-write the timestamp to the ingested time, do that now
	if m.cfg.IngestTimestamp != nil && *m.cfg.IngestTimestamp {
		e.Timestamp = time.Now()
	}

	return e
}

// Name implements Stage
func (m *wrapStage) Name() string {
	return StageTypeWrap
}
