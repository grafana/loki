package querier

import (
	"container/heap"
	"fmt"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
)

// if we are not seeing any response from ingester, how long do we want to wait by going into sleep
const nextEntryWait = time.Second / 2

// keep checking connections with ingesters in duration
const checkConnectionsWithIngestersPeriod = time.Second * 5

// dropped streams are collected into a heap to quickly find dropped stream which has oldest timestamp
type droppedStreamsIterator []logproto.DroppedStream

func (h droppedStreamsIterator) Len() int { return len(h) }
func (h droppedStreamsIterator) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}
func (h droppedStreamsIterator) Peek() time.Time {
	return h[0].From
}
func (h *droppedStreamsIterator) Push(x interface{}) {
	*h = append(*h, x.(logproto.DroppedStream))
}

func (h *droppedStreamsIterator) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h droppedStreamsIterator) Less(i, j int) bool {
	t1, t2 := h[i].From, h[j].From
	if !t1.Equal(t2) {
		return t1.Before(t2)
	}
	return h[i].Labels < h[j].Labels
}

// Tailer manages complete lifecycle of a tail request
type Tailer struct {
	// openStreamIterator is for streams already open which can be complete streams returned by ingester or
	// dropped streams queried from ingester and store
	openStreamIterator     iter.HeapIterator
	droppedStreamsIterator interface { // for holding dropped stream metadata
		heap.Interface
		Peek() time.Time
	}
	streamMtx sync.Mutex // for synchronizing access to openStreamIterator and droppedStreamsIterator

	currEntry  logproto.Entry
	currLabels string

	queryDroppedStreams       func(from, to time.Time, labels string) (iter.EntryIterator, error)
	tailDisconnectedIngesters func([]string) (map[string]logproto.Querier_TailClient, error)

	querierTailClients    map[string]logproto.Querier_TailClient // addr -> grpc clients for tailing logs from ingesters
	querierTailClientsMtx sync.Mutex

	stopped      bool
	blocked      bool
	delayFor     time.Duration
	responseChan chan<- tailResponse
	closeErrChan chan<- error

	// when tail client is slow, drop entry and store its details in droppedEntries to notify client
	droppedEntries []droppedEntry
}

func (t *Tailer) readTailClients() {
	for addr, querierTailClient := range t.querierTailClients {
		go t.readTailClient(addr, querierTailClient)
	}
}

// keeps sending oldest entry to responseChan. If channel is blocked drop the entry
// When channel is unblocked, send details of dropped entries with current entry
func (t *Tailer) loop() {
	ticker := time.NewTicker(checkConnectionsWithIngestersPeriod)
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
					level.Error(util.Logger).Log("Error closing Tailer", fmt.Sprintf("%v", err))
				}
				t.closeErrChan <- errors.New("All ingesters closed the connection")
				break
			}
			time.Sleep(nextEntryWait)
			continue
		}

		// If channel is blocked already, drop current entry directly to save the effort
		if t.blocked {
			t.dropEntry(t.currEntry.Timestamp, t.currLabels)
			continue
		}

		response := tailResponse{Stream: logproto.Stream{Labels: t.currLabels, Entries: []logproto.Entry{t.currEntry}}, DroppedEntries: t.popDroppedEntries()}
		select {
		case t.responseChan <- response:
			t.blocked = false
		default:
			t.blocked = true
			t.dropEntry(t.currEntry.Timestamp, t.currLabels)
		}
	}
}

// Checks whether we are connected to all the ingesters to tail the logs.
// Helps in connecting to disconnected ingesters or connecting to new ingesters
func (t *Tailer) checkIngesterConnections() error {
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

// removes disconnected tail client from map
func (t *Tailer) dropTailClient(addr string) {
	t.querierTailClientsMtx.Lock()
	defer t.querierTailClientsMtx.Unlock()

	delete(t.querierTailClients, addr)
}

// keeps reading streams from grpc connection with ingesters
func (t *Tailer) readTailClient(addr string, querierTailClient logproto.Querier_TailClient) {
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

// pushes new streams from ingesters synchronously
func (t *Tailer) pushTailResponseFromIngester(resp *logproto.TailResponse) {
	t.streamMtx.Lock()
	defer t.streamMtx.Unlock()

	t.openStreamIterator.Push(iter.NewStreamIterator(resp.Stream))
	if resp.DroppedStreams != nil {
		for idx := range resp.DroppedStreams {
			heap.Push(t.droppedStreamsIterator, *resp.DroppedStreams[idx])
		}
	}
}

// finds oldest entry by peeking at open stream iterator and dropped stream iterator.
// if open stream iterator has oldest entry then pop it for sending it to tail client
// else pop dropped stream details, to query from ingester and store.
// Response from ingester and store is pushed to open stream for further processing
func (t *Tailer) next() bool {
	t.streamMtx.Lock()
	defer t.streamMtx.Unlock()

	// if we don't have any entries or any of the entries are not older than now()-delay then return false
	if !((t.openStreamIterator.Len() != 0 && time.Now().After(t.openStreamIterator.Peek().Add(t.delayFor))) || (t.droppedStreamsIterator.Len() != 0 && time.Now().After(t.droppedStreamsIterator.Peek().Add(t.delayFor)))) {
		return false
	}

	// If any of the dropped streams are older than open streams, pop dropped stream details for querying them
	if t.droppedStreamsIterator.Len() != 0 {
		oldestTsFromDroppedStreams := t.droppedStreamsIterator.Peek()
		if t.droppedStreamsIterator.Len() != 0 && (t.openStreamIterator.Len() == 0 || t.openStreamIterator.Peek().After(t.droppedStreamsIterator.Peek())) {
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

func (t *Tailer) close() error {
	t.streamMtx.Lock()
	defer t.streamMtx.Unlock()

	t.stopped = true
	return t.openStreamIterator.Close()
}

func (t *Tailer) dropEntry(timestamp time.Time, labels string) {
	t.droppedEntries = append(t.droppedEntries, droppedEntry{timestamp, labels})
}

func (t *Tailer) popDroppedEntries() []droppedEntry {
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
	tailDisconnectedIngesters func([]string) (map[string]logproto.Querier_TailClient, error)) *Tailer {
	t := Tailer{
		openStreamIterator:        iter.NewHeapIterator([]iter.EntryIterator{}, logproto.FORWARD),
		droppedStreamsIterator:    &droppedStreamsIterator{},
		querierTailClients:        querierTailClients,
		queryDroppedStreams:       queryDroppedStreams,
		delayFor:                  delayFor,
		responseChan:              responseChan,
		closeErrChan:              closeErrChan,
		tailDisconnectedIngesters: tailDisconnectedIngesters,
	}

	t.readTailClients()
	go t.loop()
	return &t
}
