package stages

import (
	"errors"
	"time"

	"github.com/go-kit/kit/log"
	json "github.com/json-iterator/go"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

const ()

var ()

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
		if k == "_entry" {
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
func (p Wrapped) MarshalJSON() ([]byte, error) {

	// Marshal the entry to properly escape if it's json or contains quotes
	b, err := json.Marshal(p.Entry)
	if err != nil {
		return nil, err
	}

	// Create a map and set the already marshalled line entry
	m := map[string]json.RawMessage{
		"_entry": b,
	}

	// Add labels to the map, we do this at the top level to make querying more intuitive and easier.
	for k, v := range p.Labels {
		// Also marshal the label values to properly escape them as well
		lv, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		m[k] = lv
	}

	return json.Marshal(m)
}

// WrapConfig contains the configuration for a wrapStage
type WrapConfig struct {
	Labels          []string `mapstrcuture:"labels"`
	IngestTimestamp *bool    `mapstructure:"ingest_timestamp"`
}

// validateWrapConfig validates the WrapConfig for the wrapStage
func validateWrapConfig(cfg *WrapConfig) error {

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

	// Iterate through all the labels and extract any that match our list of labels to embed
	for lk, lv := range lbls {
		for _, wl := range m.cfg.Labels {
			if string(lk) == wl {
				wrappedLabels[wl] = string(lv)
				foundLables = append(foundLables, lk)
			}
		}
	}

	// TODO also iterate through extracted map?

	// Remove the found labels from the entry labels
	for _, fl := range foundLables {
		delete(lbls, fl)
	}

	// Embed the extracted labels into the wrapper object
	w := Wrapped{
		Labels: wrappedLabels,
		Entry:  e.Line,
	}

	// Marshal to json
	wl, err := json.Marshal(w)
	if err != nil {

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
