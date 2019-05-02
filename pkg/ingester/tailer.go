package ingester

import (
	"encoding/binary"
	"fmt"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/parser"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"hash/fnv"
	"regexp"
	"sync"
	"time"
)

const maxDroppedStreamSize = 3

type tailer struct {
	id uint32
	orgId             string
	matchers          []*labels.Matcher
	regexp            *regexp.Regexp
	c                 chan *logproto.Stream
	lastDroppedEntry  *time.Time
	closed            bool
	blockedAt         *time.Time
	blockedMtx        sync.RWMutex
	droppedStreams    []*logproto.DroppedStream
	droppedStreamsMtx sync.RWMutex

	conn logproto.Querier_TailServer
}

func newTailer(orgId, query, regex string, conn logproto.Querier_TailServer) (*tailer, error) {
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
		orgId: orgId,
		matchers: matchers,
		regexp: re,
		c: make(chan *logproto.Stream, 2),
		conn: conn,
		droppedStreams: []*logproto.DroppedStream{},
		id: generateUniqueId(orgId, query, regex),
	}, nil
}

func (t *tailer) loop() {
	var stream *logproto.Stream
	var err error

	for {
		if t.closed {
			break
		}

		stream = <- t.c
		if stream == nil {
			t.close()
			break
		}

		tailResponse := logproto.TailResponse{Stream: stream, DroppedStreams: t.popDroppedStreams()}
		err = t.conn.Send(&tailResponse)
		if err != nil {
			level.Error(util.Logger).Log("Error writing to tail client", fmt.Sprintf("%v", err))
			t.close()
			break
		}
	}
}

func (t *tailer) send(stream logproto.Stream) {
	if t.closed {
		return
	}

	if blockedSince := t.blockedSince(); blockedSince != nil {
		if blockedSince.Before(time.Now().Add(-time.Second * 15)) {
			t.close()
			close(t.c)
			return
		}
		t.dropStream(stream)
		return
	}

	if t.regexp != nil {
		t.filterEntriesInStream(&stream)
		if len(stream.Entries) == 0 {
			return
		}
	}
	select {
	case t.c <- &stream:
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
	for _, matcher := range t.matchers {
		if labelValue, ok := metric[model.LabelName(matcher.Name)]; !ok {
			return false
		} else {
			if !matcher.Matches(string(labelValue)) {
				return false
			}
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

func (t *tailer) dropStream(stream logproto.Stream)  {
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

func (t *tailer) getId() uint32 {
	return t.id
}

func breakDroppedStream(stream logproto.Stream) []*logproto.DroppedStream {
	streamLength := len(stream.Entries)
	numberOfBreaks := streamLength / maxDroppedStreamSize

	if streamLength % maxDroppedStreamSize != 0 {
		numberOfBreaks += 1
	}
	droppedStreams := make([]*logproto.DroppedStream, numberOfBreaks)
	for i:=0; i<numberOfBreaks; i++ {
		droppedStream := new(logproto.DroppedStream)
		droppedStream.From = stream.Entries[i*maxDroppedStreamSize].Timestamp
		droppedStream.To = stream.Entries[((i+1)*maxDroppedStreamSize)-1].Timestamp
		droppedStream.Labels = stream.Labels
		droppedStreams[i] = droppedStream
	}

	if streamLength % maxDroppedStreamSize != 0 {
		droppedStream := new(logproto.DroppedStream)
		droppedStream.From = stream.Entries[numberOfBreaks*maxDroppedStreamSize].Timestamp
		droppedStream.To = stream.Entries[streamLength-1].Timestamp
		droppedStreams[numberOfBreaks] = droppedStream
	}
	return droppedStreams
}

func generateUniqueId(orgId, query, regex string) uint32 {
	uniqueId := fnv.New32()
	uniqueId.Write([]byte(orgId))
	uniqueId.Write([]byte(query))
	uniqueId.Write([]byte(regex))
	timeNow := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeNow, uint64(time.Now().UnixNano()))
	uniqueId.Write(timeNow)
	return uniqueId.Sum32()
}
