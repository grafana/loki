package api

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

var (
	TestTimeStr = "2019-01-01T01:00:00.000000001Z"
	TestTime, _ = time.Parse(time.RFC3339Nano, TestTimeStr)
)

type Entry struct {
	Time   time.Time
	Log    string
	Labels model.LabelSet
}

func NewEntry(time time.Time, message string, stream string) Entry {
	return Entry{time, message, model.LabelSet{"stream": model.LabelValue(stream)}}
}

type TestCase struct {
	Line     string // input
	Error    bool
	Expected Entry
}

var criTestCases = []TestCase{
	{"", true, Entry{}},
	{TestTimeStr, true, Entry{}},
	{TestTimeStr + " stdout", true, Entry{}},
	{TestTimeStr + " stderr F", true, Entry{}},
	{TestTimeStr + " invalid F message", true, Entry{}},
	{"2019-01-01 01:00:00.000000001 stdout F message", true, Entry{}},
	{" " + TestTimeStr + " stdout F message", true, Entry{}},
	{TestTimeStr + " stdout F message", false, NewEntry(TestTime, "message", "stdout")},
	{TestTimeStr + " stderr P message", false, NewEntry(TestTime, "message", "stderr")},
	{TestTimeStr + " stderr P message1\nmessage2", false, NewEntry(TestTime, "message1\nmessage2", "stderr")},
}

func TestCRI(t *testing.T) {
	runTestCases(CRI, criTestCases, t)
}

var dockerTestCases = []TestCase{
	{
		Line:     "{{\"log\":\"bad json, should fail to parse\\n\",\"stream\":\"stderr\",\"time\":\"2019-03-04T21:37:44.789508817Z\"}",
		Error:    true,
		Expected: Entry{},
	},
	{
		Line:  "{\"log\":\"some silly log message\\n\",\"stream\":\"stderr\",\"time\":\"2019-03-04T21:37:44.789508817Z\"}",
		Error: false,
		Expected: NewEntry(time.Date(2019, 03, 04, 21, 37, 44, 789508817, time.UTC),
			"some silly log message\n",
			"stderr"),
	},
	{
		Line:  "{\"log\":\"10.15.0.5 - - [04/Mar/2019:21:37:44 +0000] \\\"POST /api/prom/push HTTP/1.1\\\" 200 0 \\\"\\\" \\\"Go-http-client/1.1\\\"\\n\",\"stream\":\"stdout\",\"time\":\"2019-03-04T21:37:44.790195228Z\"}",
		Error: false,
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
			Entries: make([]Entry, 0),
		}

		handler := parser.Wrap(client)
		err := handler.Handle(model.LabelSet{}, time.Now(), tc.Line)
		hasError := err != nil

		if tc.Error != hasError {
			t.Fatal("Parse error for test case", i, "with entry", tc.Line, "\nerror:", err)
		}

		if !tc.Error {
			if len(client.Entries) != 1 {
				t.Fatal("Handler did not receive the correct number of Entries, expected 1 received", len(client.Entries))
			}

			entry := client.Entries[0]

			if tc.Expected.Time != entry.Time {
				t.Error("Time error for test case", i, "with entry", tc.Line, "\nexpected", tc.Expected.Time, "\ngot\t\t", entry.Time)
			}

			if tc.Expected.Log != entry.Log {
				t.Error("Log entry error for test case", i, "with entry", tc.Line, "\nexpected", tc.Expected.Log, "\ngot\t\t", entry.Log)
			}

			if !tc.Expected.Labels.Equal(entry.Labels) {
				t.Error("Label error for test case", i, "with entry", tc.Line, "\nexpected", tc.Expected.Labels, "\ngot\t\t", entry.Labels)
			}
		}
	}
}

type TestClient struct {
	Entries []Entry
}

func (c *TestClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	c.Entries = append(c.Entries, Entry{t, s, ls})
	return nil
}
