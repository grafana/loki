package stages

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ww "github.com/weaveworks/common/server"

	util_log "github.com/grafana/loki/pkg/util/log"
)

// Not all these are tested but are here to make sure the different types marshal without error
var testDropYaml = `
pipeline_stages:
- json:
    expressions:
      app:
      msg:
- drop:
    sources:
      - src
    expression: ".*test.*"
    older_than: 24h
    longer_than: 8kb
- drop:
    expression: ".*app1.*"
- drop:
    sources:
      - app
    expression: loki
- drop:
    longer_than: 10000
`

func Test_dropStage_Process(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util_log.InitLogger(cfg, nil, true, false)
	Debug = true

	tests := []struct {
		name       string
		config     *DropConfig
		labels     model.LabelSet
		extracted  map[string]interface{}
		t          time.Time
		entry      string
		shouldDrop bool
	}{
		{
			name: "Longer Than Should Drop",
			config: &DropConfig{
				LongerThan: ptrFromString("10b"),
			},
			labels:     model.LabelSet{},
			extracted:  map[string]interface{}{},
			entry:      "12345678901",
			shouldDrop: true,
		},
		{
			name: "Longer Than Should Not Drop When Equal",
			config: &DropConfig{
				LongerThan: ptrFromString("10b"),
			},
			labels:     model.LabelSet{},
			extracted:  map[string]interface{}{},
			entry:      "1234567890",
			shouldDrop: false,
		},
		{
			name: "Longer Than Should Not Drop When Less",
			config: &DropConfig{
				LongerThan: ptrFromString("10b"),
			},
			labels:     model.LabelSet{},
			extracted:  map[string]interface{}{},
			entry:      "123456789",
			shouldDrop: false,
		},
		{
			name: "Older than Should Drop",
			config: &DropConfig{
				OlderThan: ptrFromString("1h"),
			},
			labels:     model.LabelSet{},
			extracted:  map[string]interface{}{},
			t:          time.Now().Add(-2 * time.Hour),
			shouldDrop: true,
		},
		{
			name: "Older than Should Not Drop",
			config: &DropConfig{
				OlderThan: ptrFromString("1h"),
			},
			labels:     model.LabelSet{},
			extracted:  map[string]interface{}{},
			t:          time.Now().Add(-5 * time.Minute),
			shouldDrop: false,
		},
		{
			name: "Matched Source",
			config: &DropConfig{
				Source: "key",
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "",
			},
			shouldDrop: true,
		},
		{
			name: "Did not match Source",
			config: &DropConfig{
				Source: "key1",
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "val1",
			},
			shouldDrop: false,
		},
		{
			name: "Matched Source and Value",
			config: &DropConfig{
				Source: "key",
				Value:  ptrFromString("val1"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "val1",
			},
			shouldDrop: true,
		},
		{
			name: "Did not match Source and Value",
			config: &DropConfig{
				Source: "key",
				Value:  ptrFromString("val1"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "VALRUE1",
			},
			shouldDrop: false,
		},
		{
			name: "Matched Source(int) and Value(string)",
			config: &DropConfig{
				Source: "level",
				Value:  ptrFromString("50"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"level": 50,
			},
			shouldDrop: true,
		},
		{
			name: "Matched Source(string) and Value(string)",
			config: &DropConfig{
				Source: "level",
				Value:  ptrFromString("50"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"level": "50",
			},
			shouldDrop: true,
		},
		{
			name: "Did not match Source(int) and Value(string)",
			config: &DropConfig{
				Source: "level",
				Value:  ptrFromString("50"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"level": 100,
			},
			shouldDrop: false,
		},
		{
			name: "Did not match Source(string) and Value(string)",
			config: &DropConfig{
				Source: "level",
				Value:  ptrFromString("50"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"level": "100",
			},
			shouldDrop: false,
		},
		{
			name: "Matched Source and Value with multiple sources",
			config: &DropConfig{
				Source: []string{"key1", "key2"},
				Value:  ptrFromString(`val1;val200.*`),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key1": "val1",
				"key2": "val200.*",
			},
			shouldDrop: true,
		},
		{
			name: "Matched Source and Value with multiple sources and custom separator",
			config: &DropConfig{
				Source:    []string{"key1", "key2"},
				Separator: ptrFromString("|"),
				Value:     ptrFromString(`val1|val200[a]`),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key1": "val1",
				"key2": "val200[a]",
			},
			shouldDrop: true,
		},
		{
			name: "Regex Matched Source(int) and Expression",
			config: &DropConfig{
				Source:     "key",
				Expression: ptrFromString("50"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": 50,
			},
			shouldDrop: true,
		},
		{
			name: "Regex Matched Source(string) and Expression",
			config: &DropConfig{
				Source:     "key",
				Expression: ptrFromString("50"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "50",
			},
			shouldDrop: true,
		},
		{
			name: "Regex Matched Source and Expression with multiple sources",
			config: &DropConfig{
				Source:     []string{"key1", "key2"},
				Expression: ptrFromString(`val\d{1};val\d{3}$`),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key1": "val1",
				"key2": "val200",
			},
			shouldDrop: true,
		},
		{
			name: "Regex Matched Source and Expression with multiple sources and custom separator",
			config: &DropConfig{
				Source:     []string{"key1", "key2"},
				Separator:  ptrFromString("#"),
				Expression: ptrFromString(`val\d{1}#val\d{3}$`),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key1": "val1",
				"key2": "val200",
			},
			shouldDrop: true,
		},
		{
			name: "Regex Did not match Source and Expression",
			config: &DropConfig{
				Source:     "key",
				Expression: ptrFromString(".*val.*"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "pal1",
			},
			shouldDrop: false,
		},
		{
			name: "Regex Did not match Source and Expression with multiple sources",
			config: &DropConfig{
				Source:     []string{"key1", "key2"},
				Expression: ptrFromString(`match\d+;match\d+`),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key1": "match1",
				"key2": "notmatch2",
			},
			shouldDrop: false,
		},
		{
			name: "Regex Did not match Source and Expression with multiple sources and custom separator",
			config: &DropConfig{
				Source:     []string{"key1", "key2"},
				Separator:  ptrFromString("#"),
				Expression: ptrFromString(`match\d;match\d`),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key1": "match1",
				"key2": "match2",
			},
			shouldDrop: false,
		},
		{
			name: "Regex No Matching Source",
			config: &DropConfig{
				Source:     "key",
				Expression: ptrFromString(".*val.*"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"pokey": "pal1",
			},
			shouldDrop: false,
		},
		{
			name: "Regex Did Not Match Line",
			config: &DropConfig{
				Expression: ptrFromString(".*val.*"),
			},
			labels:     model.LabelSet{},
			entry:      "this is a line which does not match the regex",
			extracted:  map[string]interface{}{},
			shouldDrop: false,
		},
		{
			name: "Regex Matched Line",
			config: &DropConfig{
				Expression: ptrFromString(".*val.*"),
			},
			labels:     model.LabelSet{},
			entry:      "this is a line with the word value in it",
			extracted:  map[string]interface{}{},
			shouldDrop: true,
		},
		{
			name: "Match Source and Length Both Match",
			config: &DropConfig{
				Source:     "key",
				LongerThan: ptrFromString("10b"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "pal1",
			},
			entry:      "12345678901",
			shouldDrop: true,
		},
		{
			name: "Match Source and Length Only First Matches",
			config: &DropConfig{
				Source:     "key",
				LongerThan: ptrFromString("10b"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "pal1",
			},
			entry:      "123456789",
			shouldDrop: false,
		},
		{
			name: "Match Source and Length Only Second Matches",
			config: &DropConfig{
				Source:     "key",
				LongerThan: ptrFromString("10b"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"WOOOOOOOOOOOOOO": "pal1",
			},
			entry:      "123456789012",
			shouldDrop: false,
		},
		{
			name: "Everything Must Match",
			config: &DropConfig{
				Source:     "key",
				Expression: ptrFromString(".*val.*"),
				OlderThan:  ptrFromString("1h"),
				LongerThan: ptrFromString("10b"),
			},
			labels: model.LabelSet{},
			extracted: map[string]interface{}{
				"key": "must contain value to match",
			},
			t:          time.Now().Add(-2 * time.Hour),
			entry:      "12345678901",
			shouldDrop: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDropConfig(tt.config)
			if err != nil {
				t.Error(err)
			}
			m, err := newDropStage(util_log.Logger, tt.config, prometheus.DefaultRegisterer)
			require.NoError(t, err)
			out := processEntries(m, newEntry(tt.extracted, tt.labels, tt.entry, tt.t))
			if tt.shouldDrop {
				assert.Len(t, out, 0)
			} else {
				assert.Len(t, out, 1)
			}
		})
	}
}

func ptrFromString(str string) *string {
	return &str
}

// TestDropPipeline is used to verify we properly parse the yaml config and create a working pipeline
func TestDropPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	plName := "test_pipeline"
	pl, err := NewPipeline(util_log.Logger, loadConfig(testDropYaml), &plName, registry)
	require.NoError(t, err)
	out := processEntries(pl,
		newEntry(nil, nil, testMatchLogLineApp1, time.Now()),
		newEntry(nil, nil, testMatchLogLineApp2, time.Now()),
	)

	// Only the second line will go through.
	assert.Len(t, out, 1)
	assert.Equal(t, out[0].Line, testMatchLogLineApp2)
}

var (
	dropInvalidDur      = "10y"
	dropInvalidRegex    = "(?P<ts[0-9]+).*"
	dropInvalidByteSize = "23QB"
)

func Test_validateDropConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *DropConfig
		wantErr error
	}{
		{
			name:    "ErrEmpty",
			config:  &DropConfig{},
			wantErr: errors.New(ErrDropStageEmptyConfig),
		},
		{
			name: "Invalid Duration",
			config: &DropConfig{
				OlderThan: &dropInvalidDur,
			},
			wantErr: fmt.Errorf(
				ErrDropStageInvalidDuration,
				dropInvalidDur,
				`time: unknown unit "y" in duration "10y"`,
			),
		},
		{
			name: "Invalid Regex",
			config: &DropConfig{
				Expression: &dropInvalidRegex,
			},
			wantErr: fmt.Errorf(ErrDropStageInvalidRegex, "error parsing regexp: invalid named capture: `(?P<ts[0-9]+).*`"),
		},
		{
			name: "Invalid Bytesize",
			config: &DropConfig{
				LongerThan: &dropInvalidByteSize,
			},
			wantErr: fmt.Errorf(ErrDropStageInvalidByteSize, "strconv.UnmarshalText: parsing \"23QB\": invalid syntax"),
		},
		{
			name: "Invalid Source Field Type",
			config: &DropConfig{
				Source: 1,
			},
			wantErr: errors.New(ErrDropStageInvalidSource),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateDropConfig(tt.config); ((err != nil) && (err.Error() != tt.wantErr.Error())) || (err == nil && tt.wantErr != nil) {
				t.Errorf("validateDropConfig() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
