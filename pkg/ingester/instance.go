package ingester

import (
	"context"

	"github.com/grafana/logish/pkg/logproto"
)

type instance struct {
}

func (i *instance) Push(ctx context.Context, req *logproto.WriteRequest) error {
	return nil
}
