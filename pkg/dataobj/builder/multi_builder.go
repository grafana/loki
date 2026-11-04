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
	return m.AppendInternal(tenant, *logproto.FromStream(stream), recTime)
}

// AppendInternal splits entries by ToC window while retaining their shared attributes.
func (m *MultiObjectBuilder) AppendInternal(tenant string, stream logproto.InternalStreamAdapter, recTime time.Time) error {
	streamsByTimeWindows := make(map[time.Time]logproto.InternalStreamAdapter, 1)
	stream.EachGroup(func(resource, scope []logproto.LabelAdapter, entries []logproto.Entry) {
		groups := make(map[time.Time][]logproto.Entry, 1)
		for _, entry := range entries {
			window := entry.Timestamp.UTC().Truncate(metastore.MetastoreWindowSize)
			groups[window] = append(groups[window], entry)
		}
		for window, entries := range groups {
			windowed := streamsByTimeWindows[window]
			windowed.Labels = stream.Labels
			windowed.Hash = stream.Hash
			windowed.ResourceLogs = append(windowed.ResourceLogs, logproto.ResourceLogs{
				Attrs:     resource,
				ScopeLogs: []logproto.ScopeLogs{{Attrs: scope, Entries: entries}},
			})
			streamsByTimeWindows[window] = windowed
		}
	})

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
		if err := b.AppendInternal(tenant, stream, recTime); err != nil {
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

// getBuilders returns all builders, sorted by tenant and then by ascending
// window start time.
//
// The order does not depend on Go's map iteration, so the same builders always
// come back in the same order. This makes tests and operational reasoning
// simpler.
func (m *MultiObjectBuilder) getBuilders() []builder {
	scopes := slices.SortedFunc(maps.Keys(m.builders), compareScopes)

	result := make([]builder, 0, len(scopes))
	for _, s := range scopes {
		result = append(result, m.builders[s])
	}
	return result
}
