package wire

import (
	"slices"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Collector implements [prometheus.Collector], sampling per-peer queue depths
// at scrape time. Peers register themselves via addPeer/removePeer when they
// start and stop serving.
type Collector struct {
	incomingQueueDepth prometheus.Histogram
	outgoingQueueDepth prometheus.Histogram

	peersMu sync.Mutex
	peers   []*Peer
}

var _ prometheus.Collector = (*Collector)(nil)

func NewCollector() *Collector {
	return &Collector{
		incomingQueueDepth: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_engine_wire_incoming_queue_depth",
			Help:                            "Distribution of per-peer incoming queue depth",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		outgoingQueueDepth: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_engine_wire_outgoing_queue_depth",
			Help:                            "Distribution of per-peer outgoing queue depth",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
	}
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	c.incomingQueueDepth.Describe(ch)
	c.outgoingQueueDepth.Describe(ch)
}

// Collect implements prometheus.Collector. It samples queue depths from all
// active peers and emits them as histogram observations.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.peersMu.Lock()
	peers := slices.Clone(c.peers)
	c.peersMu.Unlock()

	for _, p := range peers {
		c.incomingQueueDepth.Observe(float64(len(p.incoming)))
		c.outgoingQueueDepth.Observe(float64(len(p.outgoing)))
	}
	c.incomingQueueDepth.Collect(ch)
	c.outgoingQueueDepth.Collect(ch)
}

func (c *Collector) addPeer(p *Peer) {
	c.peersMu.Lock()
	c.peers = append(c.peers, p)
	c.peersMu.Unlock()
}

func (c *Collector) removePeer(p *Peer) {
	c.peersMu.Lock()
	defer c.peersMu.Unlock()
	for i, peer := range c.peers {
		if peer == p {
			c.peers = slices.Delete(c.peers, i, i+1)
			return
		}
	}
}
