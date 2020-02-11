package model

import (
  "time"
  "fmt"
  "encoding/json"
  "strconv"

  "github.com/prometheus/common/model"
  "github.com/grafana/loki/pkg/logproto"
)

type Entry struct {
    Ts time.Time
    Line string
}

// MarshalJSON implements the json.Marshaler interface.
func (e *Entry) MarshalJSON() ([]byte, error) {
	l, err := json.Marshal(e.Line)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("[\"%d\",%s]", e.Ts.UnixNano(), l)), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (e *Entry) UnmarshalJSON(data []byte) error {
	var unmarshal []string

	err := json.Unmarshal(data, &unmarshal)
	if err != nil {
		return err
	}

	t, err := strconv.ParseInt(unmarshal[0], 10, 64)
	if err != nil {
		return err
	}

	e.Ts = time.Unix(0, t)
	e.Line = unmarshal[1]

	return nil
}

type IntEntry struct {
    Ts int64
    Line string
}

type Entries []Entry

type LabeledEntry struct {
    Entry
    Labels model.LabelSet
}

type LabeledEntries struct {
    Entries Entries
    Labels  model.LabelSet
}

type LogEntry struct {
	LabeledEntry
	Log    string
}

type LogProtoEntry struct {
    TenantID string
    Labels model.LabelSet
    logproto.Entry
}
