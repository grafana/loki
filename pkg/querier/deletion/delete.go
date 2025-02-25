package deletion

import (
	"context"
	"time"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type DeleteGetter interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]deletion.DeleteRequest, error)
}

// DeletesForUserQuery returns the deletes for a user (taken from request context) within a given time range.
func DeletesForUserQuery(ctx context.Context, startT, endT time.Time, g DeleteGetter) ([]*logproto.Delete, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	d, err := g.GetAllDeleteRequestsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	start := startT.UnixNano()
	end := endT.UnixNano()

	var deletes []*logproto.Delete
	for _, del := range d {
		if del.StartTime.UnixNano() <= end && del.EndTime.UnixNano() >= start {
			deletes = append(deletes, &logproto.Delete{
				Selector: del.Query,
				Start:    del.StartTime.UnixNano(),
				End:      del.EndTime.UnixNano(),
			})
		}
	}

	return deletes, nil
}
