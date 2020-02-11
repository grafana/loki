package api

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lokimodel "github.com/grafana/loki/model"
)

var (
	TestTimeStr = "2019-01-01T01:00:00.000000001Z"
	TestTime, _ = time.Parse(time.RFC3339Nano, TestTimeStr)
)



func NewEntry(time time.Time, message string, stream string) lokimodel.LogEntry {
	return lokimodel.LogEntry{Ts : time, Log : message, Labels : model.LabelSet{"stream": model.LabelValue(stream)}}
}

type TestCase struct {
	Line          string // input
	ExpectedError bool
	Expected      lokimodel.LogEntry
}

var criTestCases = []TestCase{
	{"", true, lokimodel.LogEntry{}},
	{TestTimeStr, true, lokimodel.LogEntry{}},
	{TestTimeStr + " stdout", true, lokimodel.LogEntry{}},
	{TestTimeStr + " invalid F message", true, lokimodel.LogEntry{}},
	{"2019-01-01 01:00:00.000000001 stdout F message", true, lokimodel.LogEntry{}},
	{" " + TestTimeStr + " stdout F message", true, lokimodel.LogEntry{}},
	{TestTimeStr + " stdout F ", false, NewEntry(TestTime, "", "stdout")},
	{TestTimeStr + " stdout F message", false, NewEntry(TestTime, "message", "stdout")},
	{TestTimeStr + " stderr P message", false, NewEntry(TestTime, "message", "stderr")},
	{TestTimeStr + " stderr P message1\nmessage2", false, NewEntry(TestTime, "message1\nmessage2", "stderr")},
}

func TestCRI(t *testing.T) {
	runTestCases(CRI, criTestCases, t)
}

var dockerTestCases = []TestCase{
	{
		Line:          "{{\"log\":\"bad json, should fail to parse\\n\",\"stream\":\"stderr\",\"time\":\"2019-03-04T21:37:44.789508817Z\"}",
		ExpectedError: true,
		Expected:      lokimodel.LogEntry{},
	},
	{
		Line:          "{\"log\":\"some silly log message\\n\",\"stream\":\"stderr\",\"time\":\"2019-03-04T21:37:44.789508817Z\"}",
		ExpectedError: false,
		Expected: NewEntry(time.Date(2019, 03, 04, 21, 37, 44, 789508817, time.UTC),
			"some silly log message\n",
			"stderr"),
	},
	{
		Line:          "{\"log\":\"10.15.0.5 - - [04/Mar/2019:21:37:44 +0000] \\\"POST /api/prom/push HTTP/1.1\\\" 200 0 \\\"\\\" \\\"Go-http-client/1.1\\\"\\n\",\"stream\":\"stdout\",\"time\":\"2019-03-04T21:37:44.790195228Z\"}",
		ExpectedError: false,
		Expected: NewEntry(time.Date(2019, 03, 04, 21, 37, 44, 790195228, time.UTC),
			"10.15.0.5 - - [04/Mar/2019:21:37:44 +0000] \"POST /api/prom/push HTTP/1.1\" 200 0 \"\" \"Go-http-client/1.1\"\n",
			"stdout"),
	},
}

func TestDocker(t *testing.T) {
	runTestCases(Docker, dockerTestCases, t)
}

func runTestCases(parser EntryParser, testCases []TestCase, t *testing.T) {
	for i, tc := range testCases {
		client := &TestClient{
			Entries: make([]lokimodel.LogEntry, 0),
		}

		handler := parser.Wrap(client)
		err := handler.Handle(model.LabelSet{}, time.Now(), tc.Line)

		if err != nil && tc.ExpectedError {
			continue
		} else if err != nil {
			t.Fatal("Unexpected error for test case", i, "with entry", tc.Line, "\nerror:", err)
		}

		require.Equal(t, 1, len(client.Entries), "Handler did not receive the correct number of Entries for test case %d", i)
		entry := client.Entries[0]
		assert.Equal(t, tc.Expected.Ts, entry.Ts, "Time error for test case %d, with entry %s", i, tc.Line)
		assert.Equal(t, tc.Expected.Log, entry.Log, "Log entry error for test case %d, with entry %s", i, tc.Line)
		assert.True(t, tc.Expected.Labels.Equal(entry.Labels), "Label error for test case %d, labels did not match; expected: %s, found %s", i, tc.Expected.Labels, entry.Labels)
	}
}

type TestClient struct {
	Entries []lokimodel.LogEntry
}

func (c *TestClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	c.Entries = append(c.Entries, lokimodel.LogEntry{Ts :t,Log : s,Labels : ls})
	return nil
}
