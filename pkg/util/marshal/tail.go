package marshal

import (
	"github.com/grafana/loki/v3/pkg/loghttp"
	legacy "github.com/grafana/loki/v3/pkg/loghttp/legacy"
)

// NewDroppedStream constructs a DroppedStream from a legacy.DroppedEntry
func NewDroppedStream(s *legacy.DroppedEntry) (loghttp.DroppedStream, error) {
	l, err := NewLabelSet(s.Labels)
	if err != nil {
		return loghttp.DroppedStream{}, err
	}

	return loghttp.DroppedStream{
		Timestamp: s.Timestamp,
		Labels:    l,
	}, nil
}
