package builder

import (
	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

// TestBuilder implements Worker interface for testing
type TestBuilder struct {
	*Worker
}

func NewTestBuilder(builderID string, transport types.Transport) *TestBuilder {
	return &TestBuilder{
		Worker: NewWorker(builderID, transport),
	}
}
