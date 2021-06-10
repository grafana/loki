package tracing

import (
	"context"
	"os"

	"github.com/devigned/tab"

	"github.com/Azure/azure-amqp-common-go/v3/internal"
)

// StartSpanFromContext starts a span given a context and applies common library information
func StartSpanFromContext(ctx context.Context, operationName string) (context.Context, tab.Spanner) {
	ctx, span := tab.StartSpan(ctx, operationName)
	ApplyComponentInfo(span)
	return ctx, span
}

// ApplyComponentInfo applies eventhub library and network info to the span
func ApplyComponentInfo(span tab.Spanner) {
	span.AddAttributes(
		tab.StringAttribute("component", "github.com/Azure/azure-amqp-common-go"),
		tab.StringAttribute("version", common.Version))
	applyNetworkInfo(span)
}

func applyNetworkInfo(span tab.Spanner) {
	hostname, err := os.Hostname()
	if err == nil {
		span.AddAttributes(tab.StringAttribute("peer.hostname", hostname))
	}
}
