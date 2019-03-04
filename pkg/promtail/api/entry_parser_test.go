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

func NewEntry(message string, stream string) Entry {
	return Entry{TestTime, message, model.LabelSet{"stream": model.LabelValue(stream)}}
}

var criTestCases = []struct {
	Line     string // input
	Error    bool
	Expected Entry
}{
	{"", true, Entry{}},
	{TestTimeStr, true, Entry{}},
	{TestTimeStr + " stdout", true, Entry{}},
	{TestTimeStr + " stderr F", true, Entry{}},
	{TestTimeStr + " invalid F message", true, Entry{}},
	{"2019-01-01 01:00:00.000000001 stdout F message", true, Entry{}},
	{" " + TestTimeStr + " stdout F message", true, Entry{}},
	{TestTimeStr + " stdout F message", false, NewEntry("message", "stdout")},
	{TestTimeStr + " stderr P message", false, NewEntry("message", "stderr")},
	{TestTimeStr + " stderr P message1\nmessage2", false, NewEntry("message1\nmessage2", "stderr")},
}

func TestCRI(t *testing.T) {
	for _, tc := range criTestCases {
		client := &TestClient{
			Entries: make([]Entry, 0),
		}

		EntryParser := CRI.Wrap(client)
		err := EntryParser.Handle(model.LabelSet{}, time.Now(), tc.Line)
		hasError := err != nil

		if tc.Error != hasError {
			t.Error("For", tc.Line, "expected", tc.Error, "got", hasError)
		}

		if !tc.Error {
			if len(client.Entries) != 1 {
				t.Error("Handler did not receive the correct number of Entries, expected 1 received", len(client.Entries))
			}

			entry := client.Entries[0]

			if tc.Expected.Time != entry.Time {
				t.Error("For", tc.Line, "expected", tc.Expected.Time, "got", entry.Time)
			}

			if tc.Expected.Log != entry.Log {
				t.Error("For", tc.Line, "expected", tc.Expected.Log, "got", entry.Log)
			}

			if !tc.Expected.Labels.Equal(entry.Labels) {
				t.Error("For", tc.Line, "expected", tc.Expected.Labels, "got", entry.Labels)
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
