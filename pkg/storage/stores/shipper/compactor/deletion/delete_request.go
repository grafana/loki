package deletion

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type DeleteRequest struct {
	RequestID    string              `json:"request_id"`
	StartTime    model.Time          `json:"start_time"`
	EndTime      model.Time          `json:"end_time"`
	LogQLRequest string              `json:"logql_requests"`
	Status       DeleteRequestStatus `json:"status"`
	CreatedAt    model.Time          `json:"created_at"`

	UserID   string              `json:"-"`
	Matchers [][]*labels.Matcher `json:"-"`
}
