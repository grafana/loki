package stages

import (
	"testing"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	json "github.com/json-iterator/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ww "github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
)

// Not all these are tested but are here to make sure the different types marshal without error
var testWrapYaml = `
pipeline_stages:
- match:
    selector: "{container=\"foo\"}"
    stages:
    - wrap:
        labels:
          - pod
          - container
        ingest_timestamp: false
- match:
    selector: "{container=\"bar\"}"
    stages:
    - wrap:
        labels:
          - pod
          - container
        ingest_timestamp: true
`

// TestDropPipeline is used to verify we properly parse the yaml config and create a working pipeline
func TestWrapPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	plName := "test_pipeline"
	pl, err := NewPipeline(util_log.Logger, loadConfig(testWrapYaml), &plName, registry)
	require.NoError(t, err)

	l1Lbls := model.LabelSet{
		"pod":       "foo-xsfs3",
		"container": "foo",
		"namespace": "dev",
		"cluster":   "us-eu-1",
	}

	l2Lbls := model.LabelSet{
		"pod":       "foo-vvsdded",
		"container": "bar",
		"namespace": "dev",
		"cluster":   "us-eu-1",
	}

	testTime := time.Now()

	out := processEntries(pl,
		newEntry(nil, l1Lbls, testMatchLogLineApp1, testTime),
		newEntry(nil, l2Lbls, testRegexLogLine, testTime),
	)

	// Both lines should succeed
	assert.Len(t, out, 2)

	// Expected labels should remove the wrapped labels
	expectedLbls := model.LabelSet{
		"namespace": "dev",
		"cluster":   "us-eu-1",
	}
	assert.Equal(t, expectedLbls, out[0].Labels)
	assert.Equal(t, expectedLbls, out[1].Labels)

	// Validate timestamps
	// Line 1 should use the first matcher and should use the log line timestamp
	assert.Equal(t, testTime, out[0].Timestamp)
	// Line 2 should use the second matcher and should get timestamp by the wrap stage
	assert.True(t, out[1].Timestamp.After(testTime))

	// Unmarshal the wrapped object and validate line1
	w := &Wrapped{}
	assert.NoError(t, json.Unmarshal([]byte(out[0].Entry.Entry.Line), w))
	expectedWrappedLbls := map[string]string{
		"pod":       "foo-xsfs3",
		"container": "foo",
	}
	assert.Equal(t, expectedWrappedLbls, w.Labels)
	assert.Equal(t, testMatchLogLineApp1, w.Entry)

	// Validate line 2
	w = &Wrapped{}
	assert.NoError(t, json.Unmarshal([]byte(out[1].Entry.Entry.Line), w))
	expectedWrappedLbls = map[string]string{
		"pod":       "foo-vvsdded",
		"container": "bar",
	}
	assert.Equal(t, expectedWrappedLbls, w.Labels)
	assert.Equal(t, testRegexLogLine, w.Entry)
}

func Test_wrapStage_Run(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util_log.InitLogger(cfg)
	Debug = true

	tests := []struct {
		name          string
		config        *WrapConfig
		inputEntry    Entry
		expectedEntry Entry
	}{
		{
			name: "no supplied labels list",
			config: &WrapConfig{
				Labels:          nil,
				IngestTimestamp: &reallyFalse,
			},
			inputEntry: Entry{
				Extracted: map[string]interface{}{},
				Entry: api.Entry{
					Labels: model.LabelSet{
						"foo": "bar",
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "test line 1",
					},
				},
			},
			expectedEntry: Entry{
				Entry: api.Entry{
					Labels: model.LabelSet{
						"foo": "bar",
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "{\"" + entryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "match one supplied label",
			config: &WrapConfig{
				Labels:          []string{"foo"},
				IngestTimestamp: &reallyFalse,
			},
			inputEntry: Entry{
				Extracted: map[string]interface{}{},
				Entry: api.Entry{
					Labels: model.LabelSet{
						"foo": "bar",
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "test line 1",
					},
				},
			},
			expectedEntry: Entry{
				Entry: api.Entry{
					Labels: model.LabelSet{
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "{\"foo\":\"bar\",\"" + entryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "match all supplied labels",
			config: &WrapConfig{
				Labels:          []string{"foo", "bar"},
				IngestTimestamp: &reallyFalse,
			},
			inputEntry: Entry{
				Extracted: map[string]interface{}{},
				Entry: api.Entry{
					Labels: model.LabelSet{
						"foo": "bar",
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "test line 1",
					},
				},
			},
			expectedEntry: Entry{
				Entry: api.Entry{
					Labels: model.LabelSet{},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "{\"bar\":\"baz\",\"foo\":\"bar\",\"" + entryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "match extracted map and labels",
			config: &WrapConfig{
				Labels:          []string{"foo", "extr1"},
				IngestTimestamp: &reallyFalse,
			},
			inputEntry: Entry{
				Extracted: map[string]interface{}{
					"extr1": "etr1val",
					"extr2": "etr2val",
				},
				Entry: api.Entry{
					Labels: model.LabelSet{
						"foo": "bar",
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "test line 1",
					},
				},
			},
			expectedEntry: Entry{
				Entry: api.Entry{
					Labels: model.LabelSet{
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "{\"extr1\":\"etr1val\",\"foo\":\"bar\",\"" + entryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "extracted map value not convertable to a string",
			config: &WrapConfig{
				Labels:          []string{"foo", "extr2"},
				IngestTimestamp: &reallyFalse,
			},
			inputEntry: Entry{
				Extracted: map[string]interface{}{
					"extr1": "etr1val",
					"extr2": []int{1, 2, 3},
				},
				Entry: api.Entry{
					Labels: model.LabelSet{
						"foo": "bar",
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "test line 1",
					},
				},
			},
			expectedEntry: Entry{
				Entry: api.Entry{
					Labels: model.LabelSet{
						"bar": "baz",
					},
					Entry: logproto.Entry{
						Timestamp: time.Unix(1, 0),
						Line:      "{\"foo\":\"bar\",\"" + entryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWrapConfig(tt.config)
			if err != nil {
				t.Error(err)
			}
			m, err := newWrapStage(util_log.Logger, tt.config, prometheus.DefaultRegisterer)
			require.NoError(t, err)
			// Normal pipeline operation will put all the labels into the extracted map
			// replicate that here.
			for labelName, labelValue := range tt.inputEntry.Labels {
				tt.inputEntry.Extracted[string(labelName)] = string(labelValue)
			}
			out := processEntries(m, tt.inputEntry)
			// Only verify the labels, line, and timestamp, this stage doesn't modify the extracted map
			// so there is no reason to verify it
			assert.Equal(t, tt.expectedEntry.Labels, out[0].Labels)
			assert.Equal(t, tt.expectedEntry.Line, out[0].Line)
			assert.Equal(t, tt.expectedEntry.Timestamp, out[0].Timestamp)
		})
	}
}
