package client

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

var (
	walRecord = &ingester.WALRecord{
		RefEntries: make([]ingester.RefEntries, 0, 1),
		Series:     make([]record.RefSeries, 0, 1),
	}
)

const (
	errMaxStreamsLimitExceeded = "streams limit exceeded, streams: %d exceeds limit: %d, stream: '%s'"
)

// batch holds pending log streams waiting to be sent to Loki, and it's used
// to reduce the number of push requests to Loki aggregating multiple log streams
// and entries in a single batch request. In case of multi-tenant Promtail, log
// streams for each tenant are stored in a dedicated batch.
type batch struct {
	streams   map[string]*logproto.Stream
	bytes     int
	createdAt time.Time

	maxStreams int
	wal        WAL
}

func newBatch(wal WAL, maxStreams int, entries ...api.Entry) *batch {
	b := &batch{
		streams:    map[string]*logproto.Stream{},
		bytes:      0,
		createdAt:  time.Now(),
		maxStreams: maxStreams,
		wal:        wal,
	}

	// Add entries to the batch
	for _, entry := range entries {
		//never error here
		_ = b.add(entry)
	}

	return b
}

// replay adds an entry without writing it to the log, to be used
// when replaying WAL segments at startup
func (b *batch) replay(entry api.Entry) {
	labelsString := labelsMapToString(entry.Labels, ReservedLabelTenantID)
	if stream, ok := b.streams[labelsString]; ok {
		stream.Entries = append(stream.Entries, entry.Entry)
		return
	}
	// Add the entry as a new stream
	b.streams[labelsString] = &logproto.Stream{
		Labels:  labelsString,
		Entries: []logproto.Entry{entry.Entry},
	}
}

// add an entry to the batch
func (b *batch) add(entry api.Entry) error {
	b.bytes += len(entry.Line)
	walRecord.RefEntries = walRecord.RefEntries[:0]
	walRecord.Series = walRecord.Series[:0]

	// todo: log the error
	defer func() {
		err := b.wal.Log(walRecord)
		if err != nil {
			fmt.Println("err: ", err)
		}
	}()

	var fp uint64
	lbs := labels.FromMap(util.ModelLabelSetToMap(entry.Labels))
	sort.Sort(lbs)
	fp, _ = lbs.HashWithoutLabels(nil, []string(nil)...)

	// Append the entry to an already existing stream (if any)
	labelsString := labelsMapToString(entry.Labels, ReservedLabelTenantID)

	walRecord.RefEntries = append(walRecord.RefEntries, ingester.RefEntries{
		Ref: chunks.HeadSeriesRef(fp),
		Entries: []logproto.Entry{
			entry.Entry,
		},
	})
	if stream, ok := b.streams[labelsString]; ok {
		stream.Entries = append(stream.Entries, entry.Entry)
		return nil
	}
	walRecord.Series = append(walRecord.Series, record.RefSeries{
		Ref:    chunks.HeadSeriesRef(fp),
		Labels: lbs,
	})
	streams := len(b.streams)
	if b.maxStreams > 0 && streams >= b.maxStreams {
		return fmt.Errorf(errMaxStreamsLimitExceeded, streams, b.maxStreams, labelsString)
	}
	// Add the entry as a new stream
	b.streams[labelsString] = &logproto.Stream{
		Labels:  labelsString,
		Entries: []logproto.Entry{entry.Entry},
	}
	return nil
}

func labelsMapToString(ls model.LabelSet, without ...model.LabelName) string {
	lstrs := make([]string, 0, len(ls))
Outer:
	for l, v := range ls {
		for _, w := range without {
			if l == w {
				continue Outer
			}
		}
		lstrs = append(lstrs, fmt.Sprintf("%s=%q", l, v))
	}

	sort.Strings(lstrs)
	return fmt.Sprintf("{%s}", strings.Join(lstrs, ", "))
}

// sizeBytes returns the current batch size in bytes
func (b *batch) sizeBytes() int {
	return b.bytes
}

// sizeBytesAfter returns the size of the batch after the input entry
// will be added to the batch itself
func (b *batch) sizeBytesAfter(entry api.Entry) int {
	return b.bytes + len(entry.Line)
}

// age of the batch since its creation
func (b *batch) age() time.Duration {
	return time.Since(b.createdAt)
}

// encode the batch as snappy-compressed push request, and returns
// the encoded bytes and the number of encoded entries
func (b *batch) encode() ([]byte, int, error) {
	req, entriesCount := b.createPushRequest()
	buf, err := proto.Marshal(req)
	if err != nil {
		return nil, 0, err
	}
	buf = snappy.Encode(nil, buf)
	return buf, entriesCount, nil
}

// creates push request and returns it, together with number of entries
func (b *batch) createPushRequest() (*logproto.PushRequest, int) {
	req := logproto.PushRequest{
		Streams: make([]logproto.Stream, 0, len(b.streams)),
	}

	entriesCount := 0
	for _, stream := range b.streams {
		req.Streams = append(req.Streams, *stream)
		entriesCount += len(stream.Entries)
	}
	return &req, entriesCount
}
