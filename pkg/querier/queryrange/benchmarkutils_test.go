package queryrange

import (
	"sort"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type entry struct {
	entry  logproto.Entry
	labels string
}

type byDirection struct {
	direction logproto.Direction
	entries   []entry
}

func (a byDirection) Len() int      { return len(a.entries) }
func (a byDirection) Swap(i, j int) { a.entries[i], a.entries[j] = a.entries[j], a.entries[i] }
func (a byDirection) Less(i, j int) bool {
	e1, e2 := a.entries[i], a.entries[j]
	if a.direction == logproto.BACKWARD {
		switch {
		case e1.entry.Timestamp.UnixNano() < e2.entry.Timestamp.UnixNano():
			return false
		case e1.entry.Timestamp.UnixNano() > e2.entry.Timestamp.UnixNano():
			return true
		default:
			return e1.labels > e2.labels
		}
	}
	switch {
	case e1.entry.Timestamp.UnixNano() < e2.entry.Timestamp.UnixNano():
		return true
	case e1.entry.Timestamp.UnixNano() > e2.entry.Timestamp.UnixNano():
		return false
	default:
		return e1.labels < e2.labels
	}
}

func mergeStreams(resps []*LokiResponse, limit uint32, direction logproto.Direction) []logproto.Stream {
	output := byDirection{
		direction: direction,
		entries:   []entry{},
	}
	for _, resp := range resps {
		for _, stream := range resp.Data.Result {
			for _, e := range stream.Entries {
				output.entries = append(output.entries, entry{
					entry:  e,
					labels: stream.Labels,
				})
			}
		}
	}
	sort.Sort(output)
	// limit result
	if len(output.entries) >= int(limit) {
		output.entries = output.entries[:limit]
	}

	resultDict := map[string]*logproto.Stream{}
	for _, e := range output.entries {
		stream, ok := resultDict[e.labels]
		if !ok {
			stream = &logproto.Stream{
				Labels:  e.labels,
				Entries: []logproto.Entry{},
			}
			resultDict[e.labels] = stream
		}
		stream.Entries = append(stream.Entries, e.entry)

	}
	keys := make([]string, 0, len(resultDict))
	for key := range resultDict {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]logproto.Stream, 0, len(resultDict))
	for _, key := range keys {
		result = append(result, *resultDict[key])
	}

	return result
}
