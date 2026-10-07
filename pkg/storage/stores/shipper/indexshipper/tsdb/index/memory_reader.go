package index

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PlacementFunc reports whether the index file at path may be held in memory.
type PlacementFunc func(path string) bool

// PlaceAll is a PlacementFunc that allows every index file to be held in memory.
func PlaceAll(string) bool { return true }

// PlaceNone is a PlacementFunc that allows no index file to be held in memory.
func PlaceNone(string) bool { return false }

const (
	refusalReasonPlacement = "placement"
	refusalReasonBudget    = "budget"
)

// MemoryBudget caps the total number of index bytes held in memory by
// InMemoryReaders. It is safe for concurrent use and is meant to be shared by
// every InMemoryOptions in a process.
type MemoryBudget struct {
	mtx  sync.Mutex
	max  int64
	used int64

	usedBytes prometheus.Gauge
	files     *prometheus.GaugeVec
	refusals  *prometheus.CounterVec
}

// NewMemoryBudget returns a budget of maxBytes and registers its metrics with
// reg. reg may be nil, in which case the metrics are not registered.
func NewMemoryBudget(maxBytes int64, reg prometheus.Registerer) *MemoryBudget {
	f := promauto.With(reg)
	b := &MemoryBudget{
		max: maxBytes,
		usedBytes: f.NewGauge(prometheus.GaugeOpts{
			Name: "loki_tsdb_shipper_in_memory_index_bytes",
			Help: "Bytes of TSDB index files currently held in memory.",
		}),
		files: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_tsdb_shipper_index_files",
			Help: "Number of open TSDB index files, by the tier they are served from.",
		}, []string{"tier"}),
		refusals: f.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_tsdb_shipper_in_memory_index_refusals_total",
			Help: "Number of TSDB index files that were not held in memory, by reason.",
		}, []string{"reason"}),
	}
	f.NewGauge(prometheus.GaugeOpts{
		Name: "loki_tsdb_shipper_in_memory_index_budget_bytes",
		Help: "Configured maximum bytes of TSDB index files held in memory.",
	}).Set(float64(maxBytes))

	// Initialise the label values so they are exported from the start.
	b.files.WithLabelValues(string(TierMemory))
	b.files.WithLabelValues(string(TierDisk))
	b.refusals.WithLabelValues(refusalReasonPlacement)
	b.refusals.WithLabelValues(refusalReasonBudget)
	return b
}

// TryReserve reserves n bytes of the budget. It returns false, reserving
// nothing, if that would take the budget past its maximum.
func (b *MemoryBudget) TryReserve(n int64) bool {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if n < 0 || b.used+n > b.max {
		return false
	}
	b.used += n
	b.usedBytes.Set(float64(b.used))
	return true
}

// Release returns n bytes previously reserved with TryReserve.
func (b *MemoryBudget) Release(n int64) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.used -= n
	b.usedBytes.Set(float64(b.used))
}

// InMemoryOptions selects a reader that holds the whole index file in memory
// when Placement allows it and Budget has room, and otherwise opens the file
// with Fallback.
type InMemoryOptions struct {
	// Budget is shared by every reader opened with these options.
	Budget *MemoryBudget
	// Placement decides which files may be held in memory. Nil means all of them.
	Placement PlacementFunc
	// Fallback opens the files that are not held in memory.
	Fallback ReaderOptions
}

// OpenReader implements ReaderOptions.
//
// Only a placement or budget refusal falls back to Fallback. An error reading
// or decoding the file is returned as is, since the fallback reader would fail
// on the same file.
func (o InMemoryOptions) OpenReader(path string) (Reader, error) {
	if o.Placement != nil && !o.Placement(path) {
		o.Budget.refusals.WithLabelValues(refusalReasonPlacement).Inc()
		return o.openFallback(path)
	}

	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	size := fi.Size()

	// Reserve before reading so we never allocate past the budget.
	if !o.Budget.TryReserve(size) {
		o.Budget.refusals.WithLabelValues(refusalReasonBudget).Inc()
		return o.openFallback(path)
	}

	r, err := readIntoMemory(path, size)
	if err != nil {
		o.Budget.Release(size)
		return nil, err
	}

	o.Budget.files.WithLabelValues(string(TierMemory)).Inc()
	return &InMemoryReader{ByteSliceReader: r, budget: o.Budget, reserved: size}, nil
}

func readIntoMemory(path string, size int64) (*ByteSliceReader, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != size {
		return nil, fmt.Errorf("index file %s changed size while being read: expected %d bytes, read %d", path, size, len(b))
	}
	return newByteSliceReader(RealByteSlice(b), io.NopCloser(nil))
}

func (o InMemoryOptions) openFallback(path string) (Reader, error) {
	r, err := o.Fallback.OpenReader(path)
	if err != nil {
		return nil, err
	}
	o.Budget.files.WithLabelValues(string(r.Tier())).Inc()
	return &fallbackReader{Reader: r, files: o.Budget.files}, nil
}

// InMemoryReader is a ByteSliceReader over a copy of the index file held in
// process memory, counted against a MemoryBudget.
//
// Strings returned by LabelValues alias the buffer, as they do for the mmap
// reader. That is safe here because the buffer is ordinary Go heap memory: a
// string still in use after Close keeps it alive until the GC can free it.
type InMemoryReader struct {
	*ByteSliceReader

	budget   *MemoryBudget
	reserved int64
	closed   sync.Once
}

// Tier implements Reader.
func (r *InMemoryReader) Tier() Tier {
	return TierMemory
}

// Close implements Reader. It releases the reader's share of the budget; the
// buffer itself is freed by the GC once nothing references it. Calling Close
// more than once releases the budget only once.
func (r *InMemoryReader) Close() error {
	r.closed.Do(func() {
		r.budget.Release(r.reserved)
		r.budget.files.WithLabelValues(string(TierMemory)).Dec()
	})
	return r.ByteSliceReader.Close()
}

// fallbackReader wraps a reader opened by InMemoryOptions.Fallback so the
// files-by-tier gauge is decremented when it is closed.
type fallbackReader struct {
	Reader

	files  *prometheus.GaugeVec
	closed sync.Once
}

// Close implements Reader.
func (r *fallbackReader) Close() error {
	r.closed.Do(func() {
		r.files.WithLabelValues(string(r.Reader.Tier())).Dec()
	})
	return r.Reader.Close()
}
