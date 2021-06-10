package eventhub

import (
	"context"
	"net/http"
	"os"
	"strconv"

	"github.com/devigned/tab"
)

func (h *Hub) startSpanFromContext(ctx context.Context, operationName string) (tab.Spanner, context.Context) {
	ctx, span := tab.StartSpan(ctx, operationName)
	ApplyComponentInfo(span)
	return span, ctx
}

func (ns *namespace) startSpanFromContext(ctx context.Context, operationName string) (tab.Spanner, context.Context) {
	ctx, span := tab.StartSpan(ctx, operationName)
	ApplyComponentInfo(span)
	return span, ctx
}

func (s *sender) startProducerSpanFromContext(ctx context.Context, operationName string) (tab.Spanner, context.Context) {
	ctx, span := tab.StartSpan(ctx, operationName)
	ApplyComponentInfo(span)
	span.AddAttributes(
		tab.StringAttribute("span.kind", "producer"),
		tab.StringAttribute("message_bus.destination", s.getFullIdentifier()),
	)
	return span, ctx
}

func (r *receiver) startConsumerSpanFromContext(ctx context.Context, operationName string) (tab.Spanner, context.Context) {
	ctx, span := tab.StartSpan(ctx, operationName)
	ApplyComponentInfo(span)
	span.AddAttributes(
		tab.StringAttribute("span.kind", "consumer"),
		tab.StringAttribute("message_bus.destination", r.getFullIdentifier()),
	)
	return span, ctx
}

func (em *entityManager) startSpanFromContext(ctx context.Context, operationName string) (tab.Spanner, context.Context) {
	ctx, span := tab.StartSpan(ctx, operationName)
	ApplyComponentInfo(span)
	span.AddAttributes(tab.StringAttribute("span.kind", "client"))
	return span, ctx
}

// ApplyComponentInfo applies eventhub library and network info to the span
func ApplyComponentInfo(span tab.Spanner) {
	span.AddAttributes(
		tab.StringAttribute("component", "github.com/Azure/azure-event-hubs-go"),
		tab.StringAttribute("version", Version))
	applyNetworkInfo(span)
}

func applyNetworkInfo(span tab.Spanner) {
	hostname, err := os.Hostname()
	if err == nil {
		span.AddAttributes(tab.StringAttribute("peer.hostname", hostname))
	}
}

func applyRequestInfo(span tab.Spanner, req *http.Request) {
	span.AddAttributes(
		tab.StringAttribute("http.url", req.URL.String()),
		tab.StringAttribute("http.method", req.Method),
	)
}

func applyResponseInfo(span tab.Spanner, res *http.Response) {
	if res != nil {
		span.AddAttributes(tab.StringAttribute("http.status_code", strconv.Itoa(res.StatusCode)))
	}
}
