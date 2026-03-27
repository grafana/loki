package validation

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/middleware"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	GlobalPolicy = "*"

	HTTPHeaderIngestionPolicyKey   = "X-Loki-Ingestion-Policy"
	lowerIngestionPolicyHeaderName = "x-loki-ingestion-policy"
)

type PriorityStream struct {
	Priority int               `yaml:"priority" json:"priority" doc:"description=The bigger the value, the higher the priority."`
	Selector string            `yaml:"selector" json:"selector" doc:"description=Stream selector expression."`
	Matchers []*labels.Matcher `yaml:"-" json:"-"` // populated during validation.
}

func (p *PriorityStream) Matches(lbs labels.Labels) bool {
	for _, m := range p.Matchers {
		if !m.Matches(lbs.Get(m.Name)) {
			return false
		}
	}
	return true
}

type PolicyStreamMapping map[string][]*PriorityStream

func (p *PolicyStreamMapping) Validate() error {
	for policyName, policyStreams := range *p {
		for idx, policyStream := range policyStreams {
			matchers, err := syntax.ParseMatchers(policyStream.Selector, true)
			if err != nil {
				return fmt.Errorf("invalid labels matchers for policy stream mapping: %w", err)
			}
			(*p)[policyName][idx].Matchers = matchers
		}

		// Sort the mappings by priority. Higher priority mappings come first.
		slices.SortFunc(policyStreams, func(a, b *PriorityStream) int {
			return b.Priority - a.Priority
		})
	}

	return nil
}

// PolicyFor returns all the policies that matches the given labels with the highest priority.
// Note that this method will return multiple policies if two different policies match the same labels
// with the same priority.
// Returned policies are sorted alphabetically.
// If no policies match, it returns an empty slice.
// If a policy is set via the X-Loki-Ingestion-Policy header (passed through context), it overrides
// all stream-to-policy mappings and returns that policy.
func (p *PolicyStreamMapping) PolicyFor(ctx context.Context, lbs labels.Labels) []string {
	// Check if a policy was set via the HTTP header (X-Loki-Ingestion-Policy)
	// This overrides any stream-to-policy mappings
	if headerPolicy := ExtractIngestionPolicyContext(ctx); headerPolicy != "" {
		return []string{headerPolicy}
	}

	var (
		found           bool
		highestPriority int
		matchedPolicies = make(map[string]int, len(*p))
	)

	for policyName, policyStreams := range *p {
		for _, policyStream := range policyStreams {
			// Mappings are sorted by priority (see PolicyStreamMapping.Validate at this file).
			// So we can break early if the current policy has a lower priority than the highest priority matched policy.
			if found && policyStream.Priority < highestPriority {
				break
			}

			if !policyStream.Matches(lbs) {
				continue
			}

			found = true
			highestPriority = policyStream.Priority
			matchedPolicies[policyName] = policyStream.Priority
		}
	}

	// Stick with only the highest priority policies.
	policies := make([]string, 0, len(matchedPolicies))
	for policyName, priority := range matchedPolicies {
		if priority == highestPriority {
			policies = append(policies, policyName)
		}
	}

	// Sort the policies alphabetically.
	slices.Sort(policies)

	return policies
}

// ApplyDefaultPolicyStreamMappings applies default policy stream mappings to the current mapping.
// The defaults are merged with the existing mappings, with existing mappings taking precedence.
func (p *PolicyStreamMapping) ApplyDefaultPolicyStreamMappings(defaults PolicyStreamMapping) error {
	if defaults == nil {
		return nil
	}

	// If the current mapping is nil, initialize it
	if *p == nil {
		*p = make(PolicyStreamMapping)
	}

	// Merge defaults with existing mappings
	for policyName, defaultStreams := range defaults {
		if existingStreams, exists := (*p)[policyName]; exists {
			// If the policy already exists, merge the streams
			// We need to check for duplicates based on selector to avoid adding the same stream twice
			existingSelectors := make(map[string]bool)
			for _, stream := range existingStreams {
				existingSelectors[stream.Selector] = true
			}

			// Add default streams that don't already exist
			for _, defaultStream := range defaultStreams {
				if !existingSelectors[defaultStream.Selector] {
					existingStreams = append(existingStreams, defaultStream)
				}
			}
			(*p)[policyName] = existingStreams
		} else {
			// If the policy doesn't exist, copy all default streams
			streamsCopy := make([]*PriorityStream, len(defaultStreams))
			for i, stream := range defaultStreams {
				streamsCopy[i] = &PriorityStream{
					Priority: stream.Priority,
					Selector: stream.Selector,
					Matchers: stream.Matchers,
				}
			}
			(*p)[policyName] = streamsCopy
		}
	}

	// Re-validate after merging to ensure proper sorting. The defaults are already validated
	// so this should not fail, but playing it safe here and returning the error.
	if err := p.Validate(); err != nil {
		return fmt.Errorf("validation failed after merging with the defaults: %w", err)
	}
	return nil
}

// policyContextKey is used as a key for context values to avoid collisions
type policyContextKey int

const (
	ingestionPolicyContextKey policyContextKey = 1
)

// ExtractIngestionPolicyHTTP retrieves the ingestion policy from the HTTP header and returns it.
// If no policy is found, it returns an empty string.
func ExtractIngestionPolicyHTTP(r *http.Request) string {
	return r.Header.Get(HTTPHeaderIngestionPolicyKey)
}

// InjectIngestionPolicyContext returns a derived context containing the provided ingestion policy.
func InjectIngestionPolicyContext(ctx context.Context, policy string) context.Context {
	return context.WithValue(ctx, ingestionPolicyContextKey, policy)
}

// ExtractIngestionPolicyContext gets the embedded ingestion policy from the context.
// If no policy is found, it returns an empty string.
func ExtractIngestionPolicyContext(ctx context.Context) string {
	policy, ok := ctx.Value(ingestionPolicyContextKey).(string)
	if !ok {
		return ""
	}
	return policy
}

type ingestionPolicyMiddleware struct {
	logger log.Logger
}

// NewIngestionPolicyMiddleware creates a middleware that extracts the ingestion policy
// from the HTTP header and injects it into the context of the request.
func NewIngestionPolicyMiddleware(logger log.Logger) middleware.Interface {
	return &ingestionPolicyMiddleware{
		logger: logger,
	}
}

// Wrap implements the middleware interface
func (m *ingestionPolicyMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if policy := ExtractIngestionPolicyHTTP(r); policy != "" {
			r = r.Clone(InjectIngestionPolicyContext(r.Context(), policy))
		}

		next.ServeHTTP(w, r)
	})
}

// gRPC interceptor functions for propagating ingestion policy from distributor to ingester

func injectIntoGRPCRequest(ctx context.Context) (context.Context, error) {
	policy := ExtractIngestionPolicyContext(ctx)
	if policy == "" {
		return ctx, nil
	}

	// Inject into gRPC metadata
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md = md.Copy()
	md.Set(lowerIngestionPolicyHeaderName, policy)
	newCtx := metadata.NewOutgoingContext(ctx, md)

	return newCtx, nil
}

// ClientIngestionPolicyInterceptor is a gRPC unary client interceptor that propagates the ingestion policy
func ClientIngestionPolicyInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx, err := injectIntoGRPCRequest(ctx)
	if err != nil {
		return err
	}

	return invoker(ctx, method, req, reply, cc, opts...)
}

// StreamClientIngestionPolicyInterceptor is a gRPC stream client interceptor that propagates the ingestion policy
func StreamClientIngestionPolicyInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx, err := injectIntoGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return streamer(ctx, desc, cc, method, opts...)
}

func extractFromGRPCRequest(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// No metadata, just return as is
		return ctx, nil
	}

	headerValues, ok := md[lowerIngestionPolicyHeaderName]
	if !ok {
		// No ingestion policy header in metadata, just return context
		return ctx, nil
	}

	if len(headerValues) == 0 {
		return ctx, nil
	}

	// Pick first header value
	policy := headerValues[0]
	if policy == "" {
		return ctx, nil
	}

	return InjectIngestionPolicyContext(ctx, policy), nil
}

// ServerIngestionPolicyInterceptor is a gRPC unary server interceptor that extracts the ingestion policy
func ServerIngestionPolicyInterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx, err := extractFromGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

// StreamServerIngestionPolicyInterceptor is a gRPC stream server interceptor that extracts the ingestion policy
func StreamServerIngestionPolicyInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := extractFromGRPCRequest(ss.Context())
	if err != nil {
		return err
	}

	return handler(srv, serverStream{
		ctx:          ctx,
		ServerStream: ss,
	})
}

type serverStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (ss serverStream) Context() context.Context {
	return ss.ctx
}

// IngestionPoliciesKafkaProducerInterceptor extracts the ingestion policy from context and adds it as a record header to all records
func IngestionPoliciesKafkaProducerInterceptor(ctx context.Context, records []*kgo.Record) error {
	// Check once if policy exists to avoid looping if not needed
	policy := ExtractIngestionPolicyContext(ctx)
	if policy == "" {
		return nil
	}

	// Add header to all records
	header := kgo.RecordHeader{
		Key:   lowerIngestionPolicyHeaderName,
		Value: []byte(policy),
	}
	for _, record := range records {
		record.Headers = append(record.Headers, header)
	}
	return nil
}

// IngestionPoliciesKafkaHeadersToContext extracts the ingestion policy from record headers and injects it into context
func IngestionPoliciesKafkaHeadersToContext(ctx context.Context, headers []kgo.RecordHeader) context.Context {
	for _, header := range headers {
		if header.Key == lowerIngestionPolicyHeaderName {
			return InjectIngestionPolicyContext(ctx, string(header.Value))
		}
	}
	return ctx
}
