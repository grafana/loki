package stages

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ww "github.com/weaveworks/common/server"
)

func Test_MergeStage_Process(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util_log.InitLogger(cfg)
	Debug = true

	tests := []struct {
		name                string
		config              *MergeConfig
		previousExtractions map[string]interface{}
		expectedExtraction  interface{}
	}{
		{
			name: "merge multiple maps",
			config: &MergeConfig{
				Target: "test",
				Sources: []string{
					"hello",
					"foo",
				},
			},
			previousExtractions: map[string]interface{}{
				"hello": map[string]interface{}{"world": "!"},
				"foo":   map[string]interface{}{"bar": "baz"},
			},
			expectedExtraction: map[string]interface{}{"world": "!", "bar": "baz"},
		},
		{
			name: "merge map and slice",
			config: &MergeConfig{
				Target: "test",
				Sources: []string{
					"hello",
					"foo",
				},
			},
			previousExtractions: map[string]interface{}{
				"hello": map[string]interface{}{"world": "!"},
				"foo":   []interface{}{"bar", "baz"},
			},
			expectedExtraction: map[string]interface{}{
				"world": "!",
				"foo":   []interface{}{"bar", "baz"},
			},
		},
		{
			name: "merge map and string",
			config: &MergeConfig{
				Target: "test",
				Sources: []string{
					"hello",
					"foo",
				},
			},
			previousExtractions: map[string]interface{}{
				"hello": map[string]interface{}{"world": "!"},
				"foo":   "bar",
			},
			expectedExtraction: map[string]interface{}{
				"world": "!",
				"foo":   "bar",
			},
		},
		{
			name: "merge string and string",
			config: &MergeConfig{
				Target: "test",
				Sources: []string{
					"hello",
					"foo",
				},
			},
			previousExtractions: map[string]interface{}{
				"hello": "world",
				"foo":   "bar",
			},
			expectedExtraction: map[string]interface{}{
				"hello": "world",
				"foo":   "bar",
			},
		},
		{
			name: "merge with missing key returns rest of sources merged",
			config: &MergeConfig{
				Target: "test",
				Sources: []string{
					"hello",
					"missing",
				},
			},
			previousExtractions: map[string]interface{}{
				"hello": map[string]interface{}{"world": "!"},
			},
			expectedExtraction: map[string]interface{}{
				"world": "!",
			},
		},
		{
			name: "merge more than two",
			config: &MergeConfig{
				Target: "test",
				Sources: []string{
					"one",
					"two",
					"three",
				},
			},
			previousExtractions: map[string]interface{}{
				"one":   map[string]interface{}{"A": "B"},
				"two":   map[string]interface{}{"C": "D"},
				"three": map[string]interface{}{"E": "F"},
			},
			expectedExtraction: map[string]interface{}{
				"A": "B",
				"C": "D",
				"E": "F",
			},
		},
		{
			name: "maps with overlapping keys has last-write-wins",
			config: &MergeConfig{
				Target: "test",
				Sources: []string{
					"one",
					"two",
					"three",
				},
			},
			previousExtractions: map[string]interface{}{
				"one":   map[string]interface{}{"A": "B"},
				"two":   map[string]interface{}{"A": "D", "C": "E"},
				"three": map[string]interface{}{"A": "F"},
			},
			expectedExtraction: map[string]interface{}{
				"A": "F",
				"C": "E",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, err := newMergeStage(util_log.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}
			out := processEntries(st, newEntry(test.previousExtractions, nil, "", time.Now()))[0]
			assert.Equal(t, test.expectedExtraction, out.Extracted[test.config.Target])
		})
	}
}

func Test_MergeWithMissingKey_Output(t *testing.T) {
	var buf bytes.Buffer
	w := log.NewSyncWriter(&buf)
	logger := log.NewLogfmtLogger(w)
	missingKey := "missing"
	cfg := &MergeConfig{
		Target: "test",
		Sources: []string{
			"hello",
			missingKey,
		},
	}
	st, err := newMergeStage(logger, cfg)
	if err != nil {
		t.Fatal(err)
	}
	Debug = true
	_ = processEntries(st, newEntry(map[string]interface{}{"hello": "world"}, nil, "", time.Now()))
	expectedLog := fmt.Sprintf(`level=debug component=stage type=merge msg="extracted data did not contain merge source" source=%s`, missingKey)
	if !(strings.Contains(buf.String(), expectedLog)) {
		t.Errorf("\nexpected: %s\n+actual: %s", expectedLog, buf.String())
	}
}
