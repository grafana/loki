package request

import (
	"errors"
	"net/http"
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

var (
	errEndBeforeStart = errors.New("end timestamp must not be before start time")
	errNegativeStep   = errors.New("zero or negative query resolution step widths are not accepted. Try a positive integer")
	errStepTooSmall   = errors.New("exceeded maximum resolution of 11,000 points per timeseries. Try decreasing the query resolution (?step=XX)")
)

type RangeQuery struct {
	Start     time.Time
	End       time.Time
	Step      time.Duration
	Timeout   time.Duration
	Query     string
	Direction logproto.Direction
	Limit     uint32
}

func ParseRangeQuery(r *http.Request) (*RangeQuery, error) {
	var result RangeQuery
	var err error

	result.Query = query(r)
	result.Start, result.End, err = bounds(r)
	if err != nil {
		return nil, err
	}

	if result.End.Before(result.Start) {
		return nil, errEndBeforeStart
	}

	result.Limit, err = limit(r)
	if err != nil {
		return nil, err
	}

	result.Direction, err = direction(r)
	if err != nil {
		return nil, err
	}

	result.Step, err = step(r)
	if err != nil {
		return nil, err
	}

	if result.Step <= 0 {
		return nil, errNegativeStep
	}

	// For safety, limit the number of returned points per timeseries.
	// This is sufficient for 60s resolution for a week or 1h resolution for a year.
	if (result.End.Sub(result.Start) / result.Step) > 11000 {
		return nil, errStepTooSmall
	}

	return &result, nil
}
