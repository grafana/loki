// nolint:goconst
package stages

import (
	"testing"
	"time"

	ww "github.com/grafana/dskit/server"
	json "github.com/json-iterator/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// Not all these are tested but are here to make sure the different types marshal without error
var testPackYaml = `
pipeline_stages:
- match:
    selector: "{container=\"foo\"}"
    stages:
    - pack:
        labels:
          - pod
          - container
        ingest_timestamp: false
- match:
    selector: "{container=\"bar\"}"
    stages:
    - pack:
        labels:
          - pod
          - container
        ingest_timestamp: true
`

// TestPackPipeline is used to verify we properly parse the yaml config and create a working pipeline
func TestPackPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	plName := "test_pipeline_deal_with_it_linter"
	pl, err := NewPipeline(util_log.Logger, loadConfig(testPackYaml), &plName, registry)
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

	// Submit these both separately to get a deterministic output
	out1 := processEntries(pl, newEntry(nil, l1Lbls, testMatchLogLineApp1, testTime))[0]
	out2 := processEntries(pl, newEntry(nil, l2Lbls, testRegexLogLine, testTime))[0]

	// Expected labels should remove the packed labels
	expectedLbls := model.LabelSet{
		"namespace": "dev",
		"cluster":   "us-eu-1",
	}
	assert.Equal(t, expectedLbls, out1.Labels)
	assert.Equal(t, expectedLbls, out2.Labels)

	// Validate timestamps
	// Line 1 should use the first matcher and should use the log line timestamp
	assert.Equal(t, testTime, out1.Timestamp)
	// Line 2 should use the second matcher and should get timestamp by the pack stage
	assert.True(t, out2.Timestamp.After(testTime))

	// Unmarshal the packed object and validate line1
	w := &Packed{}
	assert.NoError(t, json.Unmarshal([]byte(out1.Entry.Entry.Line), w))
	expectedPackedLabels := map[string]string{
		"pod":       "foo-xsfs3",
		"container": "foo",
	}
	assert.Equal(t, expectedPackedLabels, w.Labels)
	assert.Equal(t, testMatchLogLineApp1, w.Entry)

	// Validate line 2
	w = &Packed{}
	assert.NoError(t, json.Unmarshal([]byte(out2.Entry.Entry.Line), w))
	expectedPackedLabels = map[string]string{
		"pod":       "foo-vvsdded",
		"container": "bar",
	}
	assert.Equal(t, expectedPackedLabels, w.Labels)
	assert.Equal(t, testRegexLogLine, w.Entry)
}

func Test_packStage_Run(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util_log.InitLogger(cfg, nil, false)
	Debug = true

	tests := []struct {
		name          string
		config        *PackConfig
		inputEntry    Entry
		expectedEntry Entry
	}{
		{
			name: "no supplied labels list",
			config: &PackConfig{
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
						Line:      "{\"" + logqlmodel.PackedEntryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "match one supplied label",
			config: &PackConfig{
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
						Line:      "{\"foo\":\"bar\",\"" + logqlmodel.PackedEntryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "match all supplied labels",
			config: &PackConfig{
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
						Line:      "{\"bar\":\"baz\",\"foo\":\"bar\",\"" + logqlmodel.PackedEntryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "match extracted map and labels",
			config: &PackConfig{
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
						Line:      "{\"extr1\":\"etr1val\",\"foo\":\"bar\",\"" + logqlmodel.PackedEntryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "extracted map value not convertable to a string",
			config: &PackConfig{
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
						Line:      "{\"foo\":\"bar\",\"" + logqlmodel.PackedEntryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "escape quotes",
			config: &PackConfig{
				Labels:          []string{"foo", "ex\"tr2"},
				IngestTimestamp: &reallyFalse,
			},
			inputEntry: Entry{
				Extracted: map[string]interface{}{
					"extr1":   "etr1val",
					"ex\"tr2": `"fd"`,
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
						Line:      "{\"ex\\\"tr2\":\"\\\"fd\\\"\",\"foo\":\"bar\",\"" + logqlmodel.PackedEntryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
		{
			name: "ingest timestamp",
			config: &PackConfig{
				Labels:          nil,
				IngestTimestamp: &reallyTrue,
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
						Timestamp: time.Unix(1, 0), // Ignored in test execution below
						Line:      "{\"" + logqlmodel.PackedEntryKey + "\":\"test line 1\"}",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePackConfig(tt.config)
			if err != nil {
				t.Error(err)
			}
			m, err := newPackStage(util_log.Logger, tt.config, prometheus.DefaultRegisterer)
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
			if *tt.config.IngestTimestamp {
				assert.True(t, out[0].Timestamp.After(tt.inputEntry.Timestamp))
			} else {
				assert.Equal(t, tt.expectedEntry.Timestamp, out[0].Timestamp)
			}
		})
	}
}
