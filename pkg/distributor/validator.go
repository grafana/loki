package distributor

import (
	"errors"
	"net/http"
	"time"

	cortex_client "github.com/cortexproject/cortex/pkg/ingester/client"
	cortex_validation "github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
	"github.com/weaveworks/common/httpgrpc"
)

type Validator struct {
	Limits
}

func NewValidator(l Limits) (*Validator, error) {
	if l == nil {
		return nil, errors.New("nil Limits")
	}
	return &Validator{l}, nil
}

// ValidateEntry returns an error if the entry is invalid
func (v Validator) ValidateEntry(userID string, entry logproto.Entry) error {
	if err := cortex_validation.ValidateSample(v, userID, metricName, cortex_client.Sample{
		TimestampMs: entry.Timestamp.UnixNano() / int64(time.Millisecond),
	}); err != nil {
		return err
	}

	if maxSize := v.MaxLineSize(userID); maxSize != 0 && len(entry.Line) > maxSize {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		return httpgrpc.Errorf(http.StatusBadRequest, "max line length exceeded")
	}

	return nil
}

// Validate labels returns an error if the labels are invalid
func (v Validator) ValidateLabels(userID string, labels string) error {
	ls, err := util.ToClientLabels(labels)
	if err != nil {
		// I wish we didn't return httpgrpc errors here as it seems
		// an orthogonal concept (we need not use ValidateLabels in this context)
		// but the upstream cortex_validation pkg uses it, so we keep this
		// for parity.
		return httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	return cortex_validation.ValidateLabels(v, userID, ls)
}
