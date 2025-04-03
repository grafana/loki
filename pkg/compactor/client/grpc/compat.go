package grpc

import (
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/prometheus/common/model"
)

func DeleteRequestsFromProto(deleteRequests []*DeleteRequest) []deletion.DeleteRequest {
	if len(deleteRequests) == 0 {
		return nil
	}

	out := make([]deletion.DeleteRequest, len(deleteRequests))
	for i, dr := range deleteRequests {
		out[i] = deletion.DeleteRequest{
			RequestID: dr.RequestID,
			StartTime: model.Time(dr.StartTime),
			EndTime:   model.Time(dr.EndTime),
			Query:     dr.Query,
			Status:    deletion.DeleteRequestStatus(dr.Status),
			CreatedAt: model.Time(dr.CreatedAt),
		}
	}

	return out
}

func DeleteRequestsToProto(deleteRequests []deletion.DeleteRequest) []*DeleteRequest {
	if len(deleteRequests) == 0 {
		return nil
	}

	out := make([]*DeleteRequest, len(deleteRequests))
	for i, dr := range deleteRequests {
		out[i] = &DeleteRequest{
			RequestID: dr.RequestID,
			StartTime: int64(dr.StartTime),
			EndTime:   int64(dr.EndTime),
			Query:     dr.Query,
			Status:    string(dr.Status),
			CreatedAt: int64(dr.CreatedAt),
		}
	}

	return out
}
