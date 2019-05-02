package querier

import (
	"container/heap"
	"fmt"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
	"sync"
	"time"
)

const nextEntryWait = time.Second / 2

type droppedStreamsIterator struct {
	droppedStreams []logproto.DroppedStream
	sync.Mutex
}

func (h droppedStreamsIterator) Len() int                     { return len(h.droppedStreams) }
func (h droppedStreamsIterator) Swap(i, j int)                { h.droppedStreams[i], h.droppedStreams[j] = h.droppedStreams[j], h.droppedStreams[i] }
func (h droppedStreamsIterator) Peek() time.Time {
	return h.droppedStreams[0].From
}
func (h *droppedStreamsIterator) Push(x interface{}) {
	h.droppedStreams = append(h.droppedStreams, x.(logproto.DroppedStream))
}

func (h *droppedStreamsIterator) Pop() interface{} {
	old := h.droppedStreams
	n := len(old)
	x := old[n-1]
	h.droppedStreams = old[0 : n-1]
	return x
}

func (h droppedStreamsIterator) Less(i, j int) bool {
	t1, t2 := h.droppedStreams[i].From, h.droppedStreams[j].From
	if !t1.Equal(t2) {
		return t1.Before(t2)
	}
	return h.droppedStreams[i].Labels < h.droppedStreams[j].Labels
}

type tailer struct {
	openStreamIterator iter.HeapIterator
	droppedStreamsIterator interface {
		heap.Interface
		Peek() time.Time
	}
	streamMtx sync.Mutex

	currEntry  logproto.Entry
	currLabels string

	queryDroppedStreams func(from, to time.Time, labels string) (iter.EntryIterator, error)
	tailDisconnectedIngesters func([]string) (map[string]logproto.Querier_TailClient, error)

	querierTailClients map[string]logproto.Querier_TailClient
	querierTailClientsMtx sync.Mutex

	stopped bool
	delayFor time.Duration
	responseChan chan<- tailResponse
	closeErrChan chan<- error

	droppedEntries    []droppedEntry
	droppedEntriesMtx sync.RWMutex
}

func (t *tailer) readTailClients() {
	for addr, querierTailClient := range t.querierTailClients {
		go t.readTailClient(addr, querierTailClient)
	}
}

func (t *tailer) loop()  {
	ticker := time.NewTicker(5*time.Second)
	defer ticker.Stop()

	for {
		if t.stopped {
			break
		}

		select {
		case <-ticker.C:
			// Try to reconnect dropped ingesters and connect to new ingesters
			if err := t.checkIngesterConnections(); err != nil {
				level.Error(util.Logger).Log("Error reconnecting to disconnected ingesters", fmt.Sprintf("%v", err))
			}
		default:
		}

		if !t.next() {
			if len(t.querierTailClients) == 0 {
				// All the connections to ingesters are dropped, try reconnecting or return error
				if err := t.checkIngesterConnections(); err != nil {
					level.Error(util.Logger).Log("Error reconnecting to ingesters", fmt.Sprintf("%v", err))
				} else {
					continue
				}
				if err := t.close(); err != nil {
					level.Error(util.Logger).Log("Error closing tailer", fmt.Sprintf("%v", err))
				}
				t.closeErrChan <- errors.New("All ingesters closed the connection")
				break
			}
			time.Sleep(nextEntryWait)
			continue
		}

		response := tailResponse{logproto.Stream{t.currLabels, []logproto.Entry{t.currEntry}}, t.popDroppedEntries()}
		select {
		case t.responseChan <- response:
		default:
			t.dropEntry(t.currEntry.Timestamp, t.currLabels)
		}
	}
}

func (t *tailer) checkIngesterConnections() error {
	connectedIngestersAddr := make([]string, 0, len(t.querierTailClients))
	for addr := range t.querierTailClients {
		connectedIngestersAddr = append(connectedIngestersAddr, addr)
	}

	newConnections, err := t.tailDisconnectedIngesters(connectedIngestersAddr)
	if err != nil {
		return err
	}

	if len(newConnections) != 0 {
		for addr, tailClient := range newConnections {
			t.querierTailClients[addr] = tailClient
			go t.readTailClient(addr, tailClient)
		}
	}
	return nil
}

func (t *tailer) dropTailClient(addr string)  {
	t.querierTailClientsMtx.Lock()
	defer t.querierTailClientsMtx.Unlock()

	delete(t.querierTailClients, addr)
}

func (t *tailer) readTailClient(addr string, querierTailClient logproto.Querier_TailClient) {
	var resp *logproto.TailResponse
	var err error
	defer t.dropTailClient(addr)
	for {
		if t.stopped {
			if err := querierTailClient.CloseSend(); err != nil {
				level.Error(util.Logger).Log("Error closing gprc tail client", fmt.Sprintf("%v", err))
			}
			break
		}
		resp, err = querierTailClient.Recv()
		if err != nil {
			level.Error(util.Logger).Log("Error receiving response from gprc tail client", fmt.Sprintf("%v", err))
			break
		}
		t.pushTailResponseFromIngester(resp)
	}
}

func (t *tailer) pushTailResponseFromIngester(resp *logproto.TailResponse)  {
	t.streamMtx.Lock()
	defer 	t.streamMtx.Unlock()
	t.openStreamIterator.Push(iter.NewStreamIterator(resp.Stream))
	if resp.DroppedStreams != nil {
		for idx := range resp.DroppedStreams {
			heap.Push(t.droppedStreamsIterator, *resp.DroppedStreams[idx])
		}
	}
}

func (t *tailer) next() bool {
	t.streamMtx.Lock()
	defer t.streamMtx.Unlock()

	if !((t.openStreamIterator.Len() != 0 && time.Now().After(t.openStreamIterator.Peek().Add(t.delayFor))) || (t.droppedStreamsIterator.Len() != 0 && time.Now().After(t.droppedStreamsIterator.Peek().Add(t.delayFor)))) {
		return false
	}

	// If any of the dropped streams are older than open streams, pop dropped stream and push it to open stream after querying them
	if t.droppedStreamsIterator.Len() != 0 {
		oldestTsFromDroppedStreams := t.droppedStreamsIterator.Peek()
		if time.Now().After(oldestTsFromDroppedStreams.Add(t.delayFor)) && t.droppedStreamsIterator.Len() != 0 && (t.openStreamIterator.Len() == 0 || t.openStreamIterator.Peek().After(t.droppedStreamsIterator.Peek())) {
			for t.droppedStreamsIterator.Len() != 0 && t.droppedStreamsIterator.Peek().Equal(oldestTsFromDroppedStreams) {
				droppedStream := heap.Pop(t.droppedStreamsIterator).(logproto.DroppedStream)
				iterator, err := t.queryDroppedStreams(droppedStream.From, droppedStream.To.Add(1), droppedStream.Labels)
				if err != nil {
					level.Error(util.Logger).Log("Error querying dropped streams", fmt.Sprintf("%v", err))
					continue
				}
				t.openStreamIterator.Push(iterator)
			}
		}
	}

	if !t.openStreamIterator.Next() {
		return false
	}
	t.currEntry = t.openStreamIterator.Entry()
	t.currLabels = t.openStreamIterator.Labels()
	return true
}

func (t *tailer) close() error {
	t.streamMtx.Lock()
	defer t.streamMtx.Unlock()

	t.stopped = true
	return t.openStreamIterator.Close()
}

func (t *tailer) dropEntry(timestamp time.Time, labels string)  {
	t.droppedEntries = append(t.droppedEntries, droppedEntry{timestamp, labels})
}

func (t *tailer) popDroppedEntries() []droppedEntry {
	if len(t.droppedEntries) == 0 {
		return nil
	}
	droppedEntries := t.droppedEntries
	t.droppedEntries = []droppedEntry{}

	return droppedEntries
}

func newTailer(delayFor time.Duration, querierTailClients map[string]logproto.Querier_TailClient,
	responseChan chan<- tailResponse, closeErrChan chan<- error,
	queryDroppedStreams func(from, to time.Time, labels string) (iter.EntryIterator, error),
	tailDisconnectedIngesters func([]string) (map[string]logproto.Querier_TailClient, error)) *tailer {
	t := tailer{
		openStreamIterator: iter.NewHeapIterator([]iter.EntryIterator{}, logproto.FORWARD),
		droppedStreamsIterator: &droppedStreamsIterator{},
		querierTailClients: querierTailClients,
		queryDroppedStreams: queryDroppedStreams,
		delayFor: delayFor,
		responseChan: responseChan,
		closeErrChan: closeErrChan,
		tailDisconnectedIngesters: tailDisconnectedIngesters,
	}

	t.readTailClients()
	go t.loop()
	return &t
}
