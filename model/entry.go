package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	prometheusModel "github.com/prometheus/common/model"
)

// prometheusModel.LabelSet is model of prometheus
type LabelSet prometheusModel.LabelSet

type Entry struct {
	Ts   time.Time
	Line string
}

// NewEntry constructs an Entry from a logproto.Entry
func NewEntry(e logproto.Entry) Entry {
	return Entry{
		Ts:   e.Timestamp,
		Line: e.Line,
	}
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
	Ts   int64
	Line string
}

type Entries []Entry

type LabeledEntry struct {
	Entry
	Labels LabelSet
}

type LabeledEntries struct {
	Entries Entries
	Labels  LabelSet
}

type TenantEntry struct {
	TenantID string
	Labels   LabelSet
	logproto.Entry
}
