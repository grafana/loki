package ingester

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"regexp"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/parser"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// This is to limit size of dropped stream to make it easier for querier to query.
// While dropping stream we divide it into batches and use start and end time of each batch to build dropped stream metadata
const maxDroppedStreamSize = 10000

type tailer struct {
	id       uint32
	orgID    string
	matchers []*labels.Matcher
	regexp   *regexp.Regexp

	sendChan chan *logproto.Stream
	closed   bool

	blockedAt      *time.Time
	blockedMtx     sync.RWMutex
	droppedStreams []*logproto.DroppedStream

	conn logproto.Querier_TailServer
}

func newTailer(orgID, query, regex string, conn logproto.Querier_TailServer) (*tailer, error) {
	matchers, err := parser.Matchers(query)
	if err != nil {
		return nil, err
	}

	var re *regexp.Regexp
	if regex != "" {
		re, err = regexp.Compile(regex)
		if err != nil {
			return nil, err
		}
	}

	return &tailer{
		orgID:          orgID,
		matchers:       matchers,
		regexp:         re,
		sendChan:       make(chan *logproto.Stream, 2),
		conn:           conn,
		droppedStreams: []*logproto.DroppedStream{},
		id:             generateUniqueID(orgID, query, regex),
	}, nil
}

func (t *tailer) loop() {
	var stream *logproto.Stream
	var err error

	for {
		if t.closed {
			return
		}

		stream = <-t.sendChan
		if stream == nil {
			t.close()
			return
		}

		// while sending new stream pop lined up dropped streams metadata for sending to querier
		tailResponse := logproto.TailResponse{Stream: stream, DroppedStreams: t.popDroppedStreams()}
		err = t.conn.Send(&tailResponse)
		if err != nil {
			level.Error(util.Logger).Log("Error writing to tail client", fmt.Sprintf("%v", err))
			t.close()
			return
		}
	}
}

func (t *tailer) send(stream logproto.Stream) {
	if t.closed {
		return
	}

	// if we are already dropping streams due to blocked connection, drop new streams directly to save some effort
	if blockedSince := t.blockedSince(); blockedSince != nil {
		if blockedSince.Before(time.Now().Add(-time.Second * 15)) {
			t.close()
			close(t.sendChan)
			return
		}
		t.dropStream(stream)
		return
	}

	if t.regexp != nil {
		// filter stream by regex from query
		t.filterEntriesInStream(&stream)
		if len(stream.Entries) == 0 {
			return
		}
	}

	select {
	case t.sendChan <- &stream:
	default:
		t.dropStream(stream)
	}
}

func (t *tailer) filterEntriesInStream(stream *logproto.Stream) {
	filteredEntries := new([]logproto.Entry)
	for _, entry := range stream.Entries {
		if t.regexp.MatchString(entry.Line) {
			*filteredEntries = append(*filteredEntries, entry)
		}
	}

	stream.Entries = *filteredEntries
}

func (t *tailer) isWatchingLabels(metric model.Metric) bool {
	var labelValue model.LabelValue
	var ok bool

	for _, matcher := range t.matchers {
		labelValue, ok = metric[model.LabelName(matcher.Name)]
		if !ok {
			return false
		}

		if !matcher.Matches(string(labelValue)) {
			return false
		}
	}

	return true
}

func (t *tailer) isClosed() bool {
	return t.closed
}

func (t *tailer) close() {
	t.closed = true
}

func (t *tailer) blockedSince() *time.Time {
	t.blockedMtx.RLock()
	defer t.blockedMtx.RUnlock()

	return t.blockedAt
}

func (t *tailer) dropStream(stream logproto.Stream) {
	t.blockedMtx.Lock()
	defer t.blockedMtx.Unlock()

	blockedAt := new(time.Time)
	*blockedAt = time.Now()
	t.blockedAt = blockedAt
	t.droppedStreams = append(t.droppedStreams, breakDroppedStream(stream)...)
}

func (t *tailer) popDroppedStreams() []*logproto.DroppedStream {
	t.blockedMtx.Lock()
	defer t.blockedMtx.Unlock()

	if t.blockedAt == nil {
		return nil
	}

	droppedStreams := t.droppedStreams
	t.droppedStreams = []*logproto.DroppedStream{}
	t.blockedAt = nil

	return droppedStreams
}

func (t *tailer) getID() uint32 {
	return t.id
}

func breakDroppedStream(stream logproto.Stream) []*logproto.DroppedStream {
	streamLength := len(stream.Entries)
	numberOfBreaks := streamLength / maxDroppedStreamSize

	if streamLength%maxDroppedStreamSize != 0 {
		numberOfBreaks++
	}
	droppedStreams := make([]*logproto.DroppedStream, numberOfBreaks)
	for i := 0; i < numberOfBreaks; i++ {
		droppedStream := new(logproto.DroppedStream)
		droppedStream.From = stream.Entries[i*maxDroppedStreamSize].Timestamp
		droppedStream.To = stream.Entries[((i+1)*maxDroppedStreamSize)-1].Timestamp
		droppedStream.Labels = stream.Labels
		droppedStreams[i] = droppedStream
	}

	if streamLength%maxDroppedStreamSize != 0 {
		droppedStream := new(logproto.DroppedStream)
		droppedStream.From = stream.Entries[numberOfBreaks*maxDroppedStreamSize].Timestamp
		droppedStream.To = stream.Entries[streamLength-1].Timestamp
		droppedStreams[numberOfBreaks] = droppedStream
	}
	return droppedStreams
}

// An id is useful in managing tailer instances
func generateUniqueID(orgID, query, regex string) uint32 {
	uniqueID := fnv.New32()
	uniqueID.Write([]byte(orgID))
	uniqueID.Write([]byte(query))
	uniqueID.Write([]byte(regex))

	timeNow := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeNow, uint64(time.Now().UnixNano()))
	uniqueID.Write(timeNow)

	return uniqueID.Sum32()
}
