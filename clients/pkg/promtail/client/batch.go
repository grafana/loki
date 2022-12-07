package client

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/logproto"
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
}

func newBatch(maxStreams int, entries ...api.Entry) *batch {
	b := &batch{
		streams:    map[string]*logproto.Stream{},
		bytes:      0,
		createdAt:  time.Now(),
		maxStreams: maxStreams,
	}

	// Add entries to the batch
	for _, entry := range entries {
		//never error here
		_ = b.add(entry)
	}

	return b
}

// add an entry to the batch
func (b *batch) add(entry api.Entry) error {
	b.bytes += len(entry.Line)

	// Append the entry to an already existing stream (if any)
	labels := labelsMapToString(entry.Labels, ReservedLabelTenantID)
	if stream, ok := b.streams[labels]; ok {
		stream.Entries = append(stream.Entries, entry.Entry)
		return nil
	}

	streams := len(b.streams)
	if b.maxStreams > 0 && streams >= b.maxStreams {
		return fmt.Errorf(errMaxStreamsLimitExceeded, streams, b.maxStreams, labels)
	}
	// Add the entry as a new stream
	b.streams[labels] = &logproto.Stream{
		Labels:  labels,
		Entries: []logproto.Entry{entry.Entry},
	}
	return nil
}

func labelsMapToString(ls model.LabelSet, without ...model.LabelName) string {
	var b strings.Builder
	var labelsSize int
	lstrs := make([]string, 0, len(ls))

Outer:
	for l, v := range ls {
		for _, w := range without {
			if l == w {
				continue Outer
			}
		}

		str, s := labelNameValueToString(b, l, v)
		lstrs = append(lstrs, str)
		labelsSize += s
	}
	if len(lstrs) == 0 {
		return "{}"
	}

	sort.Strings(lstrs)
	return labelListToString(b, labelsSize, lstrs)
}

// labelListToString replicates 'fmt.Sprintf("{%s}", strings.Join(lstrs, ", "))', but in a more efficent way. We achieve this by using a string.Builder and keeping track of the size at an earlier stage
func labelListToString(b strings.Builder, labelsSize int, lstrs []string) string {
	b.Reset()
	b.Grow(labelsSize + 2 + (2 * (len(lstrs) - 1)))
	b.WriteString("{")
	for i := 0; i < len(lstrs)-1; i++ {
		b.WriteString(lstrs[i])
		b.WriteString(", ")
	}
	// Write the last element seperatly so we have no trailing comma (', ')
	b.WriteString(lstrs[len(lstrs)-1])
	b.WriteString("}")
	return b.String()
}

// labelNameValueToString replicates 'fmt.Sprintf("%s=%q", l, v)', but in a more efficent way. We achieve this by using a string.Builder. We also return the size for a later concatenation.
func labelNameValueToString(b strings.Builder, l model.LabelName, v model.LabelValue) (string, int) {
	// The strings.Builder
	b.Reset()
	stringSize := len(l) + len(v) + 3 // 3 for the equals (=) and two quotes (")
	b.Grow(stringSize)
	b.WriteString(string(l))
	b.WriteString("=\"")
	b.WriteString(string(v))
	b.WriteString("\"")
	return b.String(), stringSize
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
