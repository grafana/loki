package builder

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type builderFactory interface {
	NewBuilder() (*logsobj.Builder, error)
}

// MultiObjectBuilder keeps one [logsobj.Builder] per tenant and ToC window.
type MultiObjectBuilder struct {
	builders         map[builderScope]builder
	builderFactory   builderFactory
	maxBufferedBytes int
}

// builderScope identifies the entries a single builder in a
// [MultiObjectBuilder] accepts.
type builderScope struct {
	tenant string
	window time.Time
}

func (s builderScope) String() string {
	return fmt.Sprintf("{tenant=%s, window=%s}", s.tenant, s.window.Format(time.RFC3339))
}

func compareScopes(a, b builderScope) int {
	return cmp.Or(strings.Compare(a.tenant, b.tenant), a.window.Compare(b.window))
}

// NewMultiObjectBuilder creates a new builder group scoped by tenant and ToC
// window. maxBufferedBytes is the combined upper bound across all builders.
func NewMultiObjectBuilder(builderFactory builderFactory, maxBufferedBytes int) *MultiObjectBuilder {
	return &MultiObjectBuilder{
		builders:         make(map[builderScope]builder),
		builderFactory:   builderFactory,
		maxBufferedBytes: maxBufferedBytes,
	}
}

var _ multiBuilder = (*MultiObjectBuilder)(nil)

func (m *MultiObjectBuilder) Append(tenant string, stream logproto.Stream, recTime time.Time) error {
	// [stream] might contain entries for multiple time windows => we need to split them to multiple streams, where
	// each one only contains the entries for a single time window
	streamsByTimeWindows := make(map[time.Time]logproto.Stream, 1)
	for _, e := range stream.Entries {
		w := e.Timestamp.UTC().Truncate(metastore.MetastoreWindowSize)
		if _, ok := streamsByTimeWindows[w]; !ok {
			streamsByTimeWindows[w] = logproto.Stream{
				Labels: stream.Labels,
			}
		}
		windowedStream := streamsByTimeWindows[w]
		windowedStream.Entries = append(windowedStream.Entries, e)
		streamsByTimeWindows[w] = windowedStream
	}

	for w, stream := range streamsByTimeWindows {
		scope := builderScope{tenant: tenant, window: w}
		b, exists := m.builders[scope]
		if !exists {
			var err error
			b, err = m.builderFactory.NewBuilder()
			if err != nil {
				return fmt.Errorf("error creating logsobj builder for scope %s: %w", scope, err)
			}
		}
		if err := b.Append(tenant, stream, recTime); err != nil {
			return fmt.Errorf("append for scope %s: %w", scope, err)
		}

		if !exists {
			// Only store a new builder into the map if append succeeded to avoid empty builders added to the map
			// on Append errors.
			m.builders[scope] = b
		}
	}

	return nil
}

// IsFull reports whether the combined buffered size across all builders has
// exceeded the configured target object size.
//
// We intentionally compare the *sum* (rather than each individual builder's
// IsFull) so that a partition that receives records spanning many tenants and
// windows is still bounded to roughly MaxBufferedBytes of buffered memory,
// rather than K*MaxBufferedBytes.
func (m *MultiObjectBuilder) IsFull() bool {
	return m.GetEstimatedSize() > m.maxBufferedBytes
}

func (m *MultiObjectBuilder) GetEstimatedSize() int {
	size := 0
	for _, b := range m.builders {
		size += b.GetEstimatedSize()
	}
	return size
}

func (m *MultiObjectBuilder) Reset() {
	clear(m.builders)
}

// GetBuilders returns all builders, sorted by tenant and then by ascending
// window start time.
//
// A deterministic order keeps flush-time side effects (metastore event
// writes, offset commits, and any future per-scope metrics) predictable
// regardless of Go's map iteration randomization, which simplifies both
// tests and operational reasoning.
func (m *MultiObjectBuilder) GetBuilders() []builder {
	scopes := slices.SortedFunc(maps.Keys(m.builders), compareScopes)

	result := make([]builder, 0, len(scopes))
	for _, s := range scopes {
		result = append(result, m.builders[s])
	}
	return result
}
