package labelaccess

import (
	"context"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func volume(name string, n uint64) logproto.Volume {
	return logproto.Volume{Name: name, Volume: n}
}

func volumes(resp queryrangebase.Response) []logproto.Volume {
	t, ok := resp.(*queryrange.VolumeResponse)
	if !ok || t.Response == nil {
		return nil
	}
	return t.Response.Volumes
}

// TestFilterVolumeResponse covers the response-level LBAC filter added in
// #5180. Mirrors ChunkFilterer semantics: within a policy, all matchers must
// match (AND); across policies, any matching policy passes (OR). Bare label
// names (non-series aggregation responses) are not parseable as label sets and
// must pass through — query-level injection in the HTTP middleware handles
// those.
func TestFilterVolumeResponse(t *testing.T) {
	const tenant = "test_tenant"

	policySingle := LabelPolicySet{
		tenant: []*types.LabelPolicy{
			{Selector: []*types.LabelMatcher{
				{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
			}},
		},
	}

	policyMulti := LabelPolicySet{
		tenant: []*types.LabelPolicy{
			{Selector: []*types.LabelMatcher{
				{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
			}},
			{Selector: []*types.LabelMatcher{
				{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "team", Value: "platform"},
			}},
		},
	}

	cases := []struct {
		name        string
		orgID       string // "" means do not inject orgID
		policies    LabelPolicySet
		input       *queryrange.VolumeResponse
		expectNames []string
	}{
		{
			name:     "denied entries are filtered out",
			orgID:    tenant,
			policies: policySingle,
			input: &queryrange.VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						volume(`{env="dev", job="a"}`, 10),
						volume(`{env="prod", job="b"}`, 20),
						volume(`{env="dev", job="c"}`, 30),
					},
				},
			},
			expectNames: []string{`{env="dev", job="a"}`, `{env="dev", job="c"}`},
		},
		{
			name:     "multi-policy uses OR semantics across policies",
			orgID:    tenant,
			policies: policyMulti,
			input: &queryrange.VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						volume(`{env="dev", team="other"}`, 1),     // matches policy 1
						volume(`{env="prod", team="platform"}`, 2), // matches policy 2
						volume(`{env="prod", team="other"}`, 3),    // matches neither
					},
				},
			},
			expectNames: []string{`{env="dev", team="other"}`, `{env="prod", team="platform"}`},
		},
		{
			name:     "unparseable Volume.Name passes through unchanged",
			orgID:    tenant,
			policies: policySingle,
			input: &queryrange.VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						volume("not a label set", 1), // ParseLabels rejects this
						volume(`{env="prod"}`, 2),
					},
				},
			},
			expectNames: []string{"not a label set"},
		},
		{
			name:     "no entries filtered returns the same response (no allocation churn)",
			orgID:    tenant,
			policies: policySingle,
			input: &queryrange.VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						volume(`{env="dev"}`, 1),
					},
				},
			},
			expectNames: []string{`{env="dev"}`},
		},
		{
			name:     "no orgID in ctx — response untouched",
			orgID:    "",
			policies: policySingle,
			input: &queryrange.VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						volume(`{env="prod"}`, 1),
					},
				},
			},
			expectNames: []string{`{env="prod"}`},
		},
		{
			name:  "tenant has no policies — response untouched",
			orgID: tenant,
			policies: LabelPolicySet{
				"other_tenant": []*types.LabelPolicy{
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
					}},
				},
			},
			input: &queryrange.VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						volume(`{env="prod"}`, 1),
					},
				},
			},
			expectNames: []string{`{env="prod"}`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.orgID != "" {
				ctx = user.InjectOrgID(ctx, tc.orgID)
			}

			got := filterVolumeResponse(ctx, tc.input, tc.policies)
			gotVols := volumes(got)

			gotNames := make([]string, 0, len(gotVols))
			for _, v := range gotVols {
				gotNames = append(gotNames, v.Name)
			}
			assert.ElementsMatch(t, tc.expectNames, gotNames)
		})
	}
}

// TestFilterVolumeResponse_NonVolumeResponseIsUnchanged ensures we do not
// accidentally reformat or wrap responses we don't recognize.
func TestFilterVolumeResponse_NonVolumeResponseIsUnchanged(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "test_tenant")
	policies := LabelPolicySet{
		"test_tenant": []*types.LabelPolicy{
			{Selector: []*types.LabelMatcher{
				{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
			}},
		},
	}

	resp := &queryrangebase.PrometheusResponse{Status: "success"}
	got := filterVolumeResponse(ctx, resp, policies)
	assert.Same(t, resp, got)
}

// TestNewMiddleware_FiltersVolumeResponse exercises the queryrange middleware
// from #5180 end-to-end: when LBAC policies are present in the ctx, a
// VolumeResponse returned by the downstream handler is filtered before being
// returned to the caller.
func TestNewMiddleware_FiltersVolumeResponse(t *testing.T) {
	const tenant = "test_tenant"
	policies := LabelPolicySet{
		tenant: []*types.LabelPolicy{
			{Selector: []*types.LabelMatcher{
				{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
			}},
		},
	}

	downstream := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &queryrange.VolumeResponse{
			Response: &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					volume(`{env="dev"}`, 1),
					volume(`{env="prod"}`, 2),
				},
			},
		}, nil
	})

	handler := NewMiddleware().Wrap(downstream)

	ctx := user.InjectOrgID(context.Background(), tenant)
	ctx = InjectLabelMatchersContext(ctx, policies)

	resp, err := handler.Do(ctx, &logproto.VolumeRequest{})
	require.NoError(t, err)

	got := volumes(resp)
	require.Len(t, got, 1)
	assert.Equal(t, `{env="dev"}`, got[0].Name)
}

// TestNewMiddleware_NoLBACDoesNotFilter ensures the middleware is transparent
// when no LBAC policies are set on the context. Production requests always
// reach this middleware via PropagateAllHeadersMiddleware, which seeds a
// headers map; we replicate that here by injecting an unrelated header.
func TestNewMiddleware_NoLBACDoesNotFilter(t *testing.T) {
	downstream := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &queryrange.VolumeResponse{
			Response: &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					volume(`{env="dev"}`, 1),
					volume(`{env="prod"}`, 2),
				},
			},
		}, nil
	})

	handler := NewMiddleware().Wrap(downstream)

	ctx := httpreq.InjectHeader(context.Background(), "X-Some-Other-Header", "value")
	resp, err := handler.Do(ctx, &logproto.VolumeRequest{})
	require.NoError(t, err)

	got := volumes(resp)
	assert.Len(t, got, 2)
}
