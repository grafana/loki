package labelaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"

	"github.com/grafana/loki/v3/pkg/loki/codec"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type Codec struct {
	codec.Codec
}

func NewCodec(original codec.Codec) *Codec {
	return &Codec{
		original,
	}
}

func (c *Codec) DecodeHTTPGrpcRequest(ctx context.Context, r *httpgrpc.HTTPRequest) (queryrangebase.Request, context.Context, error) {
	labelPolicySet, err := ExtractLabelMatchersGRPC(r)
	if err != nil {
		return nil, ctx, err
	}
	level.Debug(util_log.Logger).Log("msg", "extracted label policy from HTTP gRPC request", "label-policy", labelPolicySet)

	ctx = InjectLabelMatchersContext(ctx, labelPolicySet)

	return c.Codec.DecodeHTTPGrpcRequest(ctx, r)
}

func (c *Codec) EncodeRequest(ctx context.Context, req queryrangebase.Request) (*http.Request, error) {
	httpReq, err := c.Codec.EncodeRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	if auth, ok := ExtractAuthorization(ctx); ok {
		httpReq.Header.Set("Authorization", auth)
	}

	labelPolicySet, err := ExtractLabelMatchersContext(ctx)
	// This should never happen anyways because we have auth middleware before this.
	if err != nil {
		return nil, err
	}

	// If the instance policies are set on the query, ensure the user ID is decoded before leaving
	// the middleware for the scheduler and the LBAC configs are reinjected into the HTTP request
	// as headers.
	if len(labelPolicySet) != 0 {
		err = InjectLabelMatchersHTTP(httpReq, labelPolicySet)
		if err != nil {
			return nil, err
		}
	}

	return httpReq, nil
}

func (c *Codec) QueryRequestWrap(ctx context.Context, r queryrangebase.Request) (*queryrange.QueryRequest, error) {
	wrapped, err := c.Codec.QueryRequestWrap(ctx, r)
	if err != nil {
		return nil, err
	}

	if auth, ok := ExtractAuthorization(ctx); ok {
		wrapped.Metadata["Authorization"] = auth
	}

	labelPolicySet, err := ExtractLabelMatchersContext(ctx)
	// This should never happen anyways because we have auth middleware before this.
	if err != nil {
		return nil, err
	}

	// If the instance policies are set on the query, ensure the user ID is decoded before leaving
	// the middleware for the scheduler and the LBAC configs are reinjected into the HTTP request
	// as headers.
	if len(labelPolicySet) != 0 {
		marshalled, err := json.Marshal(labelPolicySet)
		if err != nil {
			return nil, err
		}
		wrapped.Metadata[HTTPHeaderKey] = string(marshalled)
	}

	return wrapped, nil
}

func (c *Codec) QueryRequestUnwrap(ctx context.Context, req *queryrange.QueryRequest) (queryrangebase.Request, context.Context, error) {

	if policy, ok := req.Metadata[HTTPHeaderKey]; ok {
		var labelPolicySet LabelPolicySet
		if err := json.Unmarshal([]byte(policy), &labelPolicySet); err != nil {
			return nil, ctx, err
		}
		ctx = InjectLabelMatchersContext(ctx, labelPolicySet)
	}

	return c.Codec.QueryRequestUnwrap(ctx, req)
}

func ExtractLabelMatchersGRPC(r *httpgrpc.HTTPRequest) (LabelPolicySet, error) {

	instancePolicyMap := LabelPolicySet{}

	// Iterate through each set header value
	for _, header := range r.Headers {
		if header.Key != HTTPHeaderKey {
			continue
		}

		for _, headerValue := range header.Values {
			// Split the header value on a comma and iterate through each policy.
			for _, v := range strings.Split(headerValue, ",") {
				instanceName, policy, err := policyFromHeaderValue(v)
				if err != nil {
					return nil, err
				}

				currentPolicies, exists := instancePolicyMap[instanceName]
				if exists {
					instancePolicyMap[instanceName] = append(currentPolicies, policy)
				} else {
					instancePolicyMap[instanceName] = []*types.LabelPolicy{policy}
				}
			}
		}
	}

	return instancePolicyMap, nil
}
