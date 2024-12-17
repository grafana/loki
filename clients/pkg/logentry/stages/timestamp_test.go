package stages

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lokiutil "github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var testTimestampYaml = `
pipeline_stages:
- json:
    expressions:
      ts: time
- timestamp:
    source: ts
    format: RFC3339
`

var testTimestampLogLine = `
{
	"time":"2012-11-01T22:08:41-04:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN"
}
`

var testTimestampLogLineWithMissingKey = `
{
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN"
}
`

func TestTimestampPipeline(t *testing.T) {
	pl, err := NewPipeline(util_log.Logger, loadConfig(testTimestampYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	out := processEntries(pl, newEntry(nil, nil, testTimestampLogLine, time.Now()))[0]
	assert.Equal(t, time.Date(2012, 11, 01, 22, 8, 41, 0, time.FixedZone("", -4*60*60)).Unix(), out.Timestamp.Unix())
}

var (
	invalidLocationString = "America/Canada"
	validLocationString   = "America/New_York"
	validLocation, _      = time.LoadLocation(validLocationString)
)

func TestPipelineWithMissingKey_Timestamp(t *testing.T) {
	var buf bytes.Buffer
	w := log.NewSyncWriter(&buf)
	logger := log.NewLogfmtLogger(w)
	pl, err := NewPipeline(logger, loadConfig(testTimestampYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	Debug = true
	_ = processEntries(pl, newEntry(nil, nil, testTimestampLogLineWithMissingKey, time.Now()))

	expectedLog := fmt.Sprintf("level=debug msg=\"%s\" err=\"Can't convert <nil> to string\" type=null", ErrTimestampConversionFailed)
	if !(strings.Contains(buf.String(), expectedLog)) {
		t.Errorf("\nexpected: %s\n+actual: %s", expectedLog, buf.String())
	}
}

func TestTimestampValidation(t *testing.T) {
	tests := map[string]struct {
		config *TimestampConfig
		// Note the error text validation is a little loose as it only validates with strings.HasPrefix
		// this is to work around different errors related to timezone loading on different systems
		err          error
		testString   string
		expectedTime time.Time
	}{
		"missing config": {
			config: nil,
			err:    errors.New(ErrEmptyTimestampStageConfig),
		},
		"missing source": {
			config: &TimestampConfig{},
			err:    errors.New(ErrTimestampSourceRequired),
		},
		"missing format": {
			config: &TimestampConfig{
				Source: "source1",
			},
			err: errors.New(ErrTimestampFormatRequired),
		},
		"invalid location": {
			config: &TimestampConfig{
				Source:   "source1",
				Format:   "2006-01-02",
				Location: &invalidLocationString,
			},
			err: fmt.Errorf(ErrInvalidLocation, ""),
		},
		"standard format": {
			config: &TimestampConfig{
				Source: "source1",
				Format: time.RFC3339,
			},
			err:          nil,
			testString:   "2012-11-01T22:08:41-04:00",
			expectedTime: time.Date(2012, 11, 01, 22, 8, 41, 0, time.FixedZone("", -4*60*60)),
		},
		"custom format with year": {
			config: &TimestampConfig{
				Source: "source1",
				Format: "2006-01-02",
			},
			err:          nil,
			testString:   "2009-01-01",
			expectedTime: time.Date(2009, 01, 01, 00, 00, 00, 0, time.UTC),
		},
		"custom format without year": {
			config: &TimestampConfig{
				Source: "source1",
				Format: "Jan 02 15:04:05",
			},
			err:          nil,
			testString:   "Jul 15 01:02:03",
			expectedTime: time.Date(time.Now().Year(), 7, 15, 1, 2, 3, 0, time.UTC),
		},
		"custom format with location": {
			config: &TimestampConfig{
				Source:   "source1",
				Format:   "2006-01-02 15:04:05",
				Location: &validLocationString,
			},
			err:          nil,
			testString:   "2009-07-01 03:30:20",
			expectedTime: time.Date(2009, 7, 1, 3, 30, 20, 0, validLocation),
		},
		"unix_ms": {
			config: &TimestampConfig{
				Source: "source1",
				Format: "UnixMs",
			},
			err:          nil,
			testString:   "1562708916919",
			expectedTime: time.Date(2019, 7, 9, 21, 48, 36, 919*1000000, time.UTC),
		},
		"should fail on invalid action on failure": {
			config: &TimestampConfig{
				Source:          "source1",
				Format:          time.RFC3339,
				ActionOnFailure: lokiutil.StringRef("foo"),
			},
			err: fmt.Errorf(ErrInvalidActionOnFailure, TimestampActionOnFailureOptions),
		},
		"fallback formats contains the format": {
			config: &TimestampConfig{
				Source:          "source1",
				Format:          "UnixMs",
				FallbackFormats: []string{"2006-01-02 03:04:05.000000000 +0000 UTC", time.RFC3339},
			},
			err:          nil,
			testString:   "2012-11-01T22:08:41-04:00",
			expectedTime: time.Date(2012, 11, 01, 22, 8, 41, 0, time.FixedZone("", -4*60*60)),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parser, err := validateTimestampConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && !strings.HasPrefix(err.Error(), test.err.Error()) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if test.testString != "" {
				ts, err := parser(test.testString)
				if err != nil {
					t.Errorf("validateOutputConfig() unexpected error parsing test time: %v", err)
					return
				}
				assert.Equal(t, test.expectedTime.UnixNano(), ts.UnixNano())
			}
		})
	}
}

func TestTimestampStage_Process(t *testing.T) {
	tests := map[string]struct {
		config    TimestampConfig
		extracted map[string]interface{}
		expected  time.Time
	}{
		"set success": {
			TimestampConfig{
				Source: "ts",
				Format: time.RFC3339,
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "2106-01-02T23:04:05-04:00",
			},
			time.Date(2106, 01, 02, 23, 04, 05, 0, time.FixedZone("", -4*60*60)),
		},
		"unix success": {
			TimestampConfig{
				Source: "ts",
				Format: "Unix",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 0, time.UTC),
		},
		"unix fractions ms success": {
			TimestampConfig{
				Source: "ts",
				Format: "Unix",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916.414123",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 414123*1000, time.UTC),
		},
		"unix fractions ns success": {
			TimestampConfig{
				Source: "ts",
				Format: "Unix",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916.000000123",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 123, time.UTC),
		},
		"unix millisecond success": {
			TimestampConfig{
				Source: "ts",
				Format: "UnixMs",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916414",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 414*1000000, time.UTC),
		},
		"unix microsecond success": {
			TimestampConfig{
				Source: "ts",
				Format: "UnixUs",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916414123",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 414123*1000, time.UTC),
		},
		"unix nano success": {
			TimestampConfig{
				Source: "ts",
				Format: "UnixNs",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916000000123",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 123, time.UTC),
		},
		"with location success": {
			TimestampConfig{
				Source:   "ts",
				Format:   "2006-01-02 15:04:05",
				Location: &validLocationString,
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "2019-07-22 20:29:32",
			},
			time.Date(2019, 7, 22, 20, 29, 32, 0, validLocation),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st, err := newTimestampStage(util_log.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}
			out := processEntries(st, newEntry(test.extracted, nil, "hello world", time.Now()))[0]
			assert.Equal(t, test.expected.UnixNano(), out.Timestamp.UnixNano())
		})
	}
}

func TestTimestampStage_ProcessActionOnFailure(t *testing.T) {
	t.Parallel()

	type inputEntry struct {
		timestamp time.Time
		labels    model.LabelSet
		extracted map[string]interface{}
	}

	tests := map[string]struct {
		config             TimestampConfig
		inputEntries       []inputEntry
		expectedTimestamps []time.Time
	}{
		"should keep the parsed timestamp on success": {
			config: TimestampConfig{
				Source:          "time",
				Format:          time.RFC3339Nano,
				ActionOnFailure: lokiutil.StringRef(TimestampActionOnFailureFudge),
			},
			inputEntries: []inputEntry{
				{timestamp: time.Unix(1, 0), extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.400000000Z"}},
				{timestamp: time.Unix(1, 0), extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.500000000Z"}},
			},
			expectedTimestamps: []time.Time{
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000000Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.500000000Z"),
			},
		},
		"should fudge the timestamp based on the last known value on timestamp parsing failure": {
			config: TimestampConfig{
				Source:          "time",
				Format:          time.RFC3339Nano,
				ActionOnFailure: lokiutil.StringRef(TimestampActionOnFailureFudge),
			},
			inputEntries: []inputEntry{
				{timestamp: time.Unix(1, 0), extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.400000000Z"}},
				{timestamp: time.Unix(1, 0), extracted: map[string]interface{}{}},
				{timestamp: time.Unix(1, 0), extracted: map[string]interface{}{}},
			},
			expectedTimestamps: []time.Time{
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000000Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000001Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000002Z"),
			},
		},
		"should fudge the timestamp based on the last known value for the right file target": {
			config: TimestampConfig{
				Source:          "time",
				Format:          time.RFC3339Nano,
				ActionOnFailure: lokiutil.StringRef(TimestampActionOnFailureFudge),
			},
			inputEntries: []inputEntry{
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"filename": "/1.log"}, extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.400000000Z"}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"filename": "/2.log"}, extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.800000000Z"}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"filename": "/1.log"}, extracted: map[string]interface{}{}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"filename": "/2.log"}, extracted: map[string]interface{}{}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"filename": "/1.log"}, extracted: map[string]interface{}{}},
			},
			expectedTimestamps: []time.Time{
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000000Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.800000000Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000001Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.800000001Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000002Z"),
			},
		},
		"should keep the input timestamp if unable to fudge because there's no known valid timestamp yet": {
			config: TimestampConfig{
				Source:          "time",
				Format:          time.RFC3339Nano,
				ActionOnFailure: lokiutil.StringRef(TimestampActionOnFailureFudge),
			},
			inputEntries: []inputEntry{
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"filename": "/1.log"}, extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.400000000Z"}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"filename": "/2.log"}, extracted: map[string]interface{}{}},
			},
			expectedTimestamps: []time.Time{
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000000Z"),
				time.Unix(1, 0),
			},
		},
		"should keep the input timestamp on action_on_failure=skip": {
			config: TimestampConfig{
				Source:          "time",
				Format:          time.RFC3339Nano,
				ActionOnFailure: lokiutil.StringRef(TimestampActionOnFailureSkip),
			},
			inputEntries: []inputEntry{
				{timestamp: time.Unix(1, 0), extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.400000000Z"}},
				{timestamp: time.Unix(1, 0), extracted: map[string]interface{}{}},
			},
			expectedTimestamps: []time.Time{
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000000Z"),
				time.Unix(1, 0),
			},
		},
		"labels with colliding fingerprints should have independent timestamps when fudging": {
			config: TimestampConfig{
				Source:          "time",
				Format:          time.RFC3339Nano,
				ActionOnFailure: lokiutil.StringRef(TimestampActionOnFailureFudge),
			},
			inputEntries: []inputEntry{
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"app": "m", "uniq0": "1", "uniq1": "1"}, extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.400000000Z"}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"app": "l", "uniq0": "0", "uniq1": "1"}, extracted: map[string]interface{}{"time": "2019-10-01T01:02:03.800000000Z"}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"app": "m", "uniq0": "1", "uniq1": "1"}, extracted: map[string]interface{}{}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"app": "l", "uniq0": "0", "uniq1": "1"}, extracted: map[string]interface{}{}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"app": "m", "uniq0": "1", "uniq1": "1"}, extracted: map[string]interface{}{}},
				{timestamp: time.Unix(1, 0), labels: model.LabelSet{"app": "l", "uniq0": "0", "uniq1": "1"}, extracted: map[string]interface{}{}},
			},
			expectedTimestamps: []time.Time{
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000000Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.800000000Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000001Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.800000001Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.400000002Z"),
				mustParseTime(time.RFC3339Nano, "2019-10-01T01:02:03.800000002Z"),
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			// Ensure the test has been correctly set
			require.Equal(t, len(testData.inputEntries), len(testData.expectedTimestamps))

			s, err := newTimestampStage(util_log.Logger, testData.config)
			require.NoError(t, err)

			for i, inputEntry := range testData.inputEntries {
				out := processEntries(s, newEntry(inputEntry.extracted, inputEntry.labels, "", inputEntry.timestamp))[0]
				assert.Equal(t, testData.expectedTimestamps[i], out.Timestamp, "entry: %d", i)
			}
		})
	}
}
