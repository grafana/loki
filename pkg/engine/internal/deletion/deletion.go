// package deletion contains utilities for handling deletion requests in query engine.
package deletion

import (
	"context"
	"time"

	"github.com/grafana/dskit/tenant"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/common/model"
)

type Getter interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string, forQuerytimeFiltering bool, timeRange *deletion.TimeRange) ([]deletionproto.DeleteRequest, error)
}

// DeletesForUserQuery returns the deletes for a user (taken from request context) within a given time range.
func DeletesForUserQuery(ctx context.Context, startT, endT time.Time, g Getter) ([]*logproto.Delete, error) {
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

	deletes := make([]*logproto.Delete, 0, len(d))
	for _, del := range d {
		deletes = append(deletes, &logproto.Delete{
			Selector: del.Query,
			Start:    del.StartTime.UnixNano(),
			End:      del.EndTime.UnixNano(),
		})
	}

	return deletes, nil
}
