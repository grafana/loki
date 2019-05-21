package ingester

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

const bufferSizeForTailResponse = 5

type tailer struct {
	id       uint32
	orgID    string
	matchers []*labels.Matcher
	filter   logql.Filter
	expr     logql.Expr

	sendChan chan *logproto.Stream
	done     chan struct{}
	closeMtx sync.Mutex

	blockedAt      *time.Time
	blockedMtx     sync.RWMutex
	droppedStreams []*logproto.DroppedStream

	conn logproto.Querier_TailServer
}

func newTailer(orgID, query string, conn logproto.Querier_TailServer) (*tailer, error) {
	expr, err := logql.ParseLogSelector(query)
	if err != nil {
		return nil, err
	}
	filter, err := expr.Filter()
	if err != nil {
		return nil, err
	}
	matchers := expr.Matchers()

	return &tailer{
		orgID:          orgID,
		matchers:       matchers,
		filter:         filter,
		sendChan:       make(chan *logproto.Stream, bufferSizeForTailResponse),
		conn:           conn,
		droppedStreams: []*logproto.DroppedStream{},
		id:             generateUniqueID(orgID, query),
		done:           make(chan struct{}),
		expr:           expr,
	}, nil
}

func (t *tailer) loop() {
	var stream *logproto.Stream
	var err error
	var ok bool

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := t.conn.Context().Err()
			if err != nil {
				t.close()
				return
			}
		case <-t.done:
			return
		case stream, ok = <-t.sendChan:
			if !ok {
				return
			} else if stream == nil {
				continue
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
}

func (t *tailer) send(stream logproto.Stream) {
	if t.isClosed() {
		return
	}

	// if we are already dropping streams due to blocked connection, drop new streams directly to save some effort
	if blockedSince := t.blockedSince(); blockedSince != nil {
		if blockedSince.Before(time.Now().Add(-time.Second * 15)) {
			t.close()
			return
		}
		t.dropStream(stream)
		return
	}

	t.filterEntriesInStream(&stream)

	if len(stream.Entries) == 0 {
		return
	}

	select {
	case t.sendChan <- &stream:
	default:
		t.dropStream(stream)
	}
}

func (t *tailer) filterEntriesInStream(stream *logproto.Stream) {
	var filteredEntries []logproto.Entry
	for _, e := range stream.Entries {
		if t.filter == nil || t.filter([]byte(e.Line)) {
			filteredEntries = append(filteredEntries, e)
		}
	}
	stream.Entries = filteredEntries
}

// Returns true if tailer is interested in the passed labelset
func (t *tailer) isWatchingLabels(metric model.Metric) bool {
	for _, matcher := range t.matchers {
		if !matcher.Matches(string(metric[model.LabelName(matcher.Name)])) {
			return false
		}
	}

	return true
}

func (t *tailer) isClosed() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *tailer) close() {
	if t.isClosed() {
		return
	}

	t.closeMtx.Lock()
	defer t.closeMtx.Unlock()

	if t.isClosed() {
		return
	}
	close(t.done)
	close(t.sendChan)
}

func (t *tailer) blockedSince() *time.Time {
	t.blockedMtx.RLock()
	defer t.blockedMtx.RUnlock()

	return t.blockedAt
}

func (t *tailer) dropStream(stream logproto.Stream) {
	if len(stream.Entries) == 0 {
		return
	}

	t.blockedMtx.Lock()
	defer t.blockedMtx.Unlock()

	if t.blockedAt == nil {
		blockedAt := time.Now()
		t.blockedAt = &blockedAt
	}
	droppedStream := logproto.DroppedStream{
		From:   stream.Entries[0].Timestamp,
		To:     stream.Entries[len(stream.Entries)-1].Timestamp,
		Labels: stream.Labels,
	}
	t.droppedStreams = append(t.droppedStreams, &droppedStream)
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

// An id is useful in managing tailer instances
func generateUniqueID(orgID, query string) uint32 {
	uniqueID := fnv.New32()
	_, _ = uniqueID.Write([]byte(orgID))
	_, _ = uniqueID.Write([]byte(query))

	timeNow := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeNow, uint64(time.Now().UnixNano()))
	_, _ = uniqueID.Write(timeNow)

	return uniqueID.Sum32()
}
