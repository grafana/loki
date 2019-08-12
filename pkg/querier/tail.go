package querier

import (
	"fmt"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
)

const (
	// keep checking connections with ingesters in duration
	checkConnectionsWithIngestersPeriod = time.Second * 5

	// the size of the channel buffer used to send tailing streams
	// back to the requesting client
	maxBufferedTailResponses = 10

	// the maximum number of entries to return in a TailResponse
	maxEntriesPerTailResponse = 100

	// the maximum number of dropped entries to keep in memory that will be sent along
	// with the next successfully pushed response. Once the dropped entries memory buffer
	// exceed this value, we start skipping dropped entries too.
	maxDroppedEntriesPerTailResponse = 1000
)

type droppedEntry struct {
	Timestamp time.Time
	Labels    string
}

// TailResponse holds response sent by tailer
type TailResponse struct {
	Streams        []logproto.Stream `json:"streams"`
	DroppedEntries []droppedEntry    `json:"dropped_entries"`
}

// Tailer manages complete lifecycle of a tail request
type Tailer struct {
	// openStreamIterator is for streams already open
	openStreamIterator iter.HeapIterator
	streamMtx          sync.Mutex // for synchronizing access to openStreamIterator

	currEntry  logproto.Entry
	currLabels string

	tailDisconnectedIngesters func([]string) (map[string]logproto.Querier_TailClient, error)

	querierTailClients    map[string]logproto.Querier_TailClient // addr -> grpc clients for tailing logs from ingesters
	querierTailClientsMtx sync.Mutex

	stopped         bool
	delayFor        time.Duration
	responseChan    chan *TailResponse
	closeErrChan    chan error
	tailMaxDuration time.Duration

	// if we are not seeing any response from ingester,
	// how long do we want to wait by going into sleep
	waitEntryThrottle time.Duration
}

func (t *Tailer) readTailClients() {
	for addr, querierTailClient := range t.querierTailClients {
		go t.readTailClient(addr, querierTailClient)
	}
}

// keeps sending oldest entry to responseChan. If channel is blocked drop the entry
// When channel is unblocked, send details of dropped entries with current entry
func (t *Tailer) loop() {
	checkConnectionTicker := time.NewTicker(checkConnectionsWithIngestersPeriod)
	defer checkConnectionTicker.Stop()

	tailMaxDurationTicker := time.NewTicker(t.tailMaxDuration)
	defer tailMaxDurationTicker.Stop()

	droppedEntries := make([]droppedEntry, 0)

	for !t.stopped {
		select {
		case <-checkConnectionTicker.C:
			// Try to reconnect dropped ingesters and connect to new ingesters
			if err := t.checkIngesterConnections(); err != nil {
				level.Error(util.Logger).Log("msg", "Error reconnecting to disconnected ingesters", "err", err)
			}
		case <-tailMaxDurationTicker.C:
			if err := t.close(); err != nil {
				level.Error(util.Logger).Log("msg", "Error closing Tailer", "err", err)
			}
			t.closeErrChan <- errors.New("reached tail max duration limit")
			return
		default:
		}

		// Read as much entries as we can (up to the max allowed) and populate the
		// tail response we'll send over the response channel
		tailResponse := new(TailResponse)
		entriesCount := 0

		for ; entriesCount < maxEntriesPerTailResponse && t.next(); entriesCount++ {
			// If the response channel channel is blocked, we drop the current entry directly
			// to save the effort
			if t.isResponseChanBlocked() {
				droppedEntries = dropEntry(droppedEntries, t.currEntry.Timestamp, t.currLabels)
				continue
			}

			tailResponse.Streams = append(tailResponse.Streams, logproto.Stream{
				Labels:  t.currLabels,
				Entries: []logproto.Entry{t.currEntry},
			})
		}

		// If all consumed entries have been dropped because the response channel is blocked
		// we should reiterate on the loop
		if len(tailResponse.Streams) == 0 && entriesCount > 0 {
			continue
		}

		// If no entry has been consumed we should ensure it's not caused by all ingesters
		// connections dropped and then throttle for a while
		if len(tailResponse.Streams) == 0 {
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
				t.closeErrChan <- errors.New("all ingesters closed the connection")
				return
			}

			time.Sleep(t.waitEntryThrottle)
			continue
		}

		// Send the tail response through the response channel without blocking.
		// Drop the entry if the response channel buffer is full.
		if len(droppedEntries) > 0 {
			tailResponse.DroppedEntries = droppedEntries
		}

		select {
		case t.responseChan <- tailResponse:
			if len(droppedEntries) > 0 {
				droppedEntries = make([]droppedEntry, 0)
			}
		default:
			droppedEntries = dropEntries(droppedEntries, tailResponse.Streams)
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
}

// finds oldest entry by peeking at open stream iterator.
// Response from ingester is pushed to open stream for further processing
func (t *Tailer) next() bool {
	t.streamMtx.Lock()
	defer t.streamMtx.Unlock()

	if t.openStreamIterator.Len() == 0 || !time.Now().After(t.openStreamIterator.Peek().Add(t.delayFor)) || !t.openStreamIterator.Next() {
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

func (t *Tailer) isResponseChanBlocked() bool {
	// Thread-safety: len() and cap() on a channel are thread-safe. The cap() doesn't
	// change over the time, while len() does.
	return len(t.responseChan) == cap(t.responseChan)
}

func (t *Tailer) getResponseChan() <-chan *TailResponse {
	return t.responseChan
}

func (t *Tailer) getCloseErrorChan() <-chan error {
	return t.closeErrChan
}

func newTailer(
	delayFor time.Duration,
	querierTailClients map[string]logproto.Querier_TailClient,
	historicEntries iter.EntryIterator,
	tailDisconnectedIngesters func([]string) (map[string]logproto.Querier_TailClient, error),
	tailMaxDuration time.Duration,
	waitEntryThrottle time.Duration,
) *Tailer {
	t := Tailer{
		openStreamIterator:        iter.NewHeapIterator([]iter.EntryIterator{historicEntries}, logproto.FORWARD),
		querierTailClients:        querierTailClients,
		delayFor:                  delayFor,
		responseChan:              make(chan *TailResponse, maxBufferedTailResponses),
		closeErrChan:              make(chan error),
		tailDisconnectedIngesters: tailDisconnectedIngesters,
		tailMaxDuration:           tailMaxDuration,
		waitEntryThrottle:         waitEntryThrottle,
	}

	t.readTailClients()
	go t.loop()
	return &t
}

func dropEntry(droppedEntries []droppedEntry, timestamp time.Time, labels string) []droppedEntry {
	if len(droppedEntries) >= maxDroppedEntriesPerTailResponse {
		return droppedEntries
	}

	return append(droppedEntries, droppedEntry{timestamp, labels})
}

func dropEntries(droppedEntries []droppedEntry, streams []logproto.Stream) []droppedEntry {
	for _, stream := range streams {
		for _, entry := range stream.Entries {
			droppedEntries = dropEntry(droppedEntries, entry.Timestamp, entry.Line)
		}
	}

	return droppedEntries
}
