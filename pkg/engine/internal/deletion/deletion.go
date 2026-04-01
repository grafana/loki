// package deletion contains utilities for handling deletion requests in query engine.
package deletion

import (
	"context"
	"time"

	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
)

// Getter defines methods to get deletion requests.
type Getter interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string, forQuerytimeFiltering bool, timeRange *deletion.TimeRange) ([]deletionproto.DeleteRequest, error)
}

// Request represents a deletion request.
type Request struct {
	Selector string
	Start    int64
	End      int64
}

// DeletesForUser returns the delete reqs for a user (taken from request context) overlapping the provided time range.
func DeletesForUser(ctx context.Context, startT, endT time.Time, g Getter) ([]*Request, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// forQuerytimeFiltering is set to false as we want to get all deletes in the time range, not just pending ones.
	d, err := g.GetAllDeleteRequestsForUser(ctx, userID, false, &deletion.TimeRange{
		Start: model.TimeFromUnixNano(startT.UnixNano()),
		End:   model.TimeFromUnixNano(endT.UnixNano()),
	})
	if err != nil {
		return nil, err
	}

	deletes := make([]*Request, 0, len(d))
	for _, del := range d {
		deletes = append(deletes, &Request{
			Selector: del.Query,
			Start:    del.StartTime.UnixNano(),
			End:      del.EndTime.UnixNano(),
		})
	}

	return deletes, nil
}
