// TODO(grobinson): Find a way to move this file into the dataobj package.
// Today it's not possible because there is a circular dependency between the
// dataobj package and the logs package.
package tools

import (
	"context"
	"errors"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

type Stats struct {
	Size     uint64
	Sections int
	Tenants  []string
}

// ReadStats returns statistics about the data object. ReadStats returns an
// error if the data object couldn't be inspected or if the provided ctx is
// canceled.
func ReadStats(ctx context.Context, obj *dataobj.Object) (*Stats, error) {
	s := Stats{}
	s.Size = uint64(obj.Size())
	for _, sec := range obj.Sections() {
		s.Sections++
		switch {
		case streams.CheckSection(sec):
			streamsSec, err := streams.Open(ctx, sec)
			if err != nil {
				return nil, err
			}
			s.Tenants = append(s.Tenants, streamsSec.Tenant())
		case logs.CheckSection(sec):
			logsSec, err := logs.Open(ctx, sec)
			if err != nil {
				return nil, err
			}
			s.Tenants = append(s.Tenants, logsSec.Tenant())
		default:
			return nil, errors.New("unknown section type")
		}
	}
	// A tenant can have multiple sections, so we must deduplicate them.
	slices.Sort(s.Tenants)
	slices.Compact(s.Tenants)
	return &s, nil
}
